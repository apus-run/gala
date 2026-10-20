#!/usr/bin/env python3
"""Isolated, bounded real-server compatibility and process-crash checks."""
import datetime
import json
import os
from pathlib import Path
import secrets
import shutil
import tempfile
import subprocess
import sys
import time
import urllib.request

HERE = Path(__file__).resolve().parent
MODULE = HERE.parent
REPO = HERE.parents[4]
OUT = REPO / 'audit' / 'rustfs'
OUT.mkdir(parents=True, exist_ok=True)
RUN = 'gala-rustfs-' + secrets.token_hex(5)
IMAGE = os.getenv('RUSTFS_IMAGE', 'rustfs/rustfs:1.0.3')
env = dict(os.environ, RUSTFS_ACCESS_KEY='gala'+secrets.token_hex(8), RUSTFS_SECRET_KEY=secrets.token_hex(24), RUSTFS_TEST_BUCKET=RUN, GOWORK='off')
result = {'started_at': datetime.datetime.now(datetime.timezone.utc).isoformat(), 'image_requested': IMAGE, 'stages': []}
container = None
volume = None

def command(name, args, timeout=60, extra=None, show=False, allow_failure=False):
 print(name, flush=True)
 started = time.monotonic()
 try:
  p = subprocess.run(args, cwd=MODULE, env=env | (extra or {}), text=True, capture_output=True, timeout=timeout)
  output = p.stdout + p.stderr
  (OUT / (name+'.txt')).write_text(output)
  result['stages'].append({'name':name,'exit_code':p.returncode,'seconds':round(time.monotonic()-started,3)})
  if show: print(output[-3000:], flush=True)
  if p.returncode and not allow_failure: raise RuntimeError(f'{name}: exit {p.returncode}; see {OUT/name}.txt')
  return p.stdout
 except subprocess.TimeoutExpired:
  result['stages'].append({'name':name,'result':'TIMEOUT','seconds':round(time.monotonic()-started,3)})
  raise RuntimeError(name+': timed out')

def ready(name):
 started=time.monotonic();last=''
 while time.monotonic()-started<60:
  try:
   with urllib.request.urlopen(env['RUSTFS_TEST_ENDPOINT']+'/health/ready',timeout=2) as r:
    if r.status==200:
     (OUT/(name+'.txt')).write_bytes(r.read());result['stages'].append({'name':name,'exit_code':0,'seconds':round(time.monotonic()-started,3)});return
  except Exception as e: last=type(e).__name__
  time.sleep(1)
 raise RuntimeError(name+': readiness timed out; last='+last)

try:
 result['engine']=json.loads(command('engine',['docker','info','--format','{"version":{{json .ServerVersion}},"os":{{json .OperatingSystem}},"cpus":{{.NCPU}},"memory":{{.MemTotal}}}'],timeout=10))
 command('pull',['docker','pull',IMAGE],timeout=240,show=True)
 result['image']=json.loads(command('image',['docker','image','inspect',IMAGE,'--format','{"id":{{json .Id}},"digests":{{json .RepoDigests}},"architecture":{{json .Architecture}},"user":{{json .Config.User}}}'],timeout=10))
 # Pin this particular run to the resolved immutable image ID.
 env['RUSTFS_IMAGE']=result['image']['id']
 container=RUN+'-rustfs-1';volume=RUN+'_data'
 command('start',['docker','compose','-p',RUN,'-f',str(HERE/'compose.yaml'),'up','-d'],timeout=60)
 address=command('port',['docker','port',container,'9000/tcp'],timeout=10).strip()
 env['RUSTFS_TEST_ENDPOINT']='http://'+address
 ready('ready')
 command('contract',['go','test','-race','-json','-count=1','-timeout=180s','-run','^TestRustFS','.'],timeout=210,allow_failure=True)
 events=[]
 for line in (OUT/'contract.txt').read_text().splitlines():
  try: events.append(json.loads(line))
  except ValueError: pass
 result['tests']=[{'name':e['Test'],'result':e['Action'],'seconds':e.get('Elapsed')} for e in events if e.get('Test') and e.get('Action') in ('pass','fail','skip')]
 if not any(t['result']=='pass' and t['name']=='TestRustFSContract' for t in result['tests']): raise RuntimeError('real contract tests did not run')
 # Ablate the two fixes against the same real server, one factor at a time.
 with tempfile.TemporaryDirectory(prefix='gala-rustfs-ablation-') as tmp:
  mutations=[
   ('status-zero','objectstore/s3/errors.go','errors.As(err, &sc) && sc.HTTPStatusCode() > 0','errors.As(err, &sc)'),
   ('commit-retry','objectstore/s3/upload.go','func singleAttempt(o *awss3.Options) { o.RetryMaxAttempts = 1 }','func singleAttempt(o *awss3.Options) {}'),
  ]
  result['ablations']=[]
  for name,file,old,new in mutations:
   root=Path(tmp)/name
   shutil.copytree(MODULE.parents[1],root)
   target=root/file;source=target.read_text()
   if source.count(old)!=1: raise RuntimeError(name+': mutation must match exactly once')
   target.write_text(source.replace(old,new,1))
   command('ablation-'+name,['go','-C',str(root/'objectstore/s3'),'test','-json','-count=1','-timeout=45s','-run','^TestRustFSLostCommitResponse$','.'],timeout=60,allow_failure=True)
   stage=result['stages'][-1]
   events=[json.loads(line) for line in (OUT/('ablation-'+name+'.txt')).read_text().splitlines() if line.startswith('{')]
   killed=stage['exit_code']!=0 and any(e.get('Action')=='fail' and e.get('Test','').startswith('TestRustFSLostCommitResponse') for e in events)
   if not killed: raise RuntimeError(name+': mutation survived or experiment was invalid')
   stage['expected_failure']=True
   result['ablations'].append({'name':name,'result':'KILLED'})
 command('persistence-write',['go','test','-v','-count=1','-timeout=60s','-run','^TestRustFSPersistence$','.'],timeout=75,extra={'RUSTFS_TEST_PHASE':'write'})
 command('sigkill',['docker','kill','--signal=KILL',container],timeout=10)
 command('restart',['docker','start',container],timeout=30)
 address=command('port-after-kill',['docker','port',container,'9000/tcp'],timeout=10).strip();env['RUSTFS_TEST_ENDPOINT']='http://'+address
 ready('ready-after-kill')
 command('persistence-after-kill',['go','test','-v','-count=1','-timeout=60s','-run','^TestRustFSPersistence$','.'],timeout=75,extra={'RUSTFS_TEST_PHASE':'read'})
 command('recreate',['docker','compose','-p',RUN,'-f',str(HERE/'compose.yaml'),'up','-d','--force-recreate'],timeout=60)
 address=command('port-recreated',['docker','port',container,'9000/tcp'],timeout=10).strip();env['RUSTFS_TEST_ENDPOINT']='http://'+address
 ready('ready-after-recreate')
 command('persistence-after-recreate',['go','test','-v','-count=1','-timeout=60s','-run','^TestRustFSPersistence$','.'],timeout=75,extra={'RUSTFS_TEST_PHASE':'read'})
 result['status']='FAIL' if any(s.get('exit_code',0)!=0 and not s.get('expected_failure') for s in result['stages']) else 'PASS'
except Exception as e:
 result['status']='FAIL';result['error']=str(e);print(str(e),flush=True)
finally:
 if container:
  try:
   command('server-logs',['docker','logs',container],timeout=10)
   command('runtime',['docker','inspect',container,'--format','{"restart_count":{{.RestartCount}},"state":{{json .State}},"memory_limit":{{.HostConfig.Memory}},"cpu_limit":{{.HostConfig.NanoCpus}}}'],timeout=10)
  except Exception: pass
  try: command('cleanup',['docker','compose','-p',RUN,'-f',str(HERE/'compose.yaml'),'down','--volumes'],timeout=30)
  except Exception as e: result['cleanup_error']=str(e)
 result['finished_at']=datetime.datetime.now(datetime.timezone.utc).isoformat()
 (OUT/'results.json').write_text(json.dumps(result,ensure_ascii=False,indent=2)+'\n')
 print('Result:',result['status'],'Evidence:',OUT,flush=True)
sys.exit(0 if result['status']=='PASS' else 1)
