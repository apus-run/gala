#!/usr/bin/env python3
"""Four-node real-server quorum experiment; only this run's containers are killed."""
import datetime
import json
import os
from pathlib import Path
import secrets
import subprocess
import sys
import time
import urllib.error
import urllib.request

HERE=Path(__file__).resolve().parent
OUT=HERE.parents[4]/'audit/rustfs/cluster'
OUT.mkdir(parents=True,exist_ok=True)
project='gala-rustfs-ha-'+secrets.token_hex(4)
env=dict(os.environ,RUSTFS_IMAGE=os.getenv('RUSTFS_IMAGE','rustfs/rustfs:1.0.3'),RUSTFS_ACCESS_KEY='gala'+secrets.token_hex(8),RUSTFS_SECRET_KEY=secrets.token_hex(24),RUSTFS_TEST_BUCKET=project,GOWORK='off')
base=['docker','compose','-p',project,'-f',str(HERE/'cluster.yaml')]
result={'started_at':datetime.datetime.now(datetime.timezone.utc).isoformat(),'topology':'4 nodes x 4 logical drive paths, one volume per node on one Docker VM','stages':[]}

def run(name,args,timeout=60,extra=None):
 print(name,flush=True);start=time.monotonic()
 p=subprocess.run(args,cwd=HERE.parent,env=env|(extra or {}),capture_output=True,text=True,timeout=timeout)
 (OUT/(name+'.txt')).write_text(p.stdout+p.stderr)
 result['stages'].append({'name':name,'exit_code':p.returncode,'seconds':round(time.monotonic()-start,3)})
 if p.returncode:raise RuntimeError(name+' failed; see '+str(OUT/(name+'.txt')))
 return p.stdout

def endpoint(node):
 return 'http://'+run('port-'+str(node),base+['port','rustfs'+str(node),'9000'],timeout=10).strip()

def wait_ready(name,url):
 start=time.monotonic()
 while time.monotonic()-start<90:
  state=subprocess.run(['docker','inspect',project+'-rustfs3-1','--format','{{.State.Status}}'],capture_output=True,text=True,timeout=5)
  if state.stdout.strip()=='exited': raise RuntimeError(name+': target node exited; see logs')
  try:
   with urllib.request.urlopen(url+'/health/ready',timeout=2) as r:
    if r.status==200:
     (OUT/(name+'.json')).write_bytes(r.read());result['stages'].append({'name':name,'exit_code':0,'seconds':round(time.monotonic()-start,3)});return
  except Exception:pass
  time.sleep(1)
 raise RuntimeError(name+' timed out')

def go(name,test,phase,timeout=90):
 run(name,['go','test','-race','-v','-count=1','-timeout=60s','-run',test,'.'],timeout=timeout,extra={'RUSTFS_TEST_PHASE':phase,'RUSTFS_TEST_SKIP_CREATE':'0' if phase=='write' else '1'})
try:
 image=run('image',['docker','image','inspect',env['RUSTFS_IMAGE'],'--format','{{.Id}}'],timeout=10).strip();env['RUSTFS_IMAGE']=image;result['image_id']=image
 run('start',base+['up','-d'],timeout=90)
 env['RUSTFS_TEST_ENDPOINT']=endpoint(3);wait_ready('ready',env['RUSTFS_TEST_ENDPOINT'])
 go('write-all-nodes','^TestRustFSPersistence$','write')
 run('kill-one',base+['kill','-s','SIGKILL','rustfs1'],timeout=10)
 wait_ready('ready-one-node-down',env['RUSTFS_TEST_ENDPOINT'])
 go('read-one-node-down','^TestRustFSPersistence$','read')
 go('write-one-node-down','^TestRustFSPersistence$','write-degraded')
 go('read-new-objects-one-node-down','^TestRustFSPersistence$','read-degraded')
 run('kill-two',base+['kill','-s','SIGKILL','rustfs2'],timeout=10)
  # Readiness is a read-quorum signal; inspect writeQuorum explicitly.
 for attempt in range(20):
  try:
   response=urllib.request.urlopen(env['RUSTFS_TEST_ENDPOINT']+'/health/ready',timeout=2)
  except urllib.error.HTTPError as e: response=e
  state=json.loads(response.read());response.close()
  if state.get('details',{}).get('storage',{}).get('writeQuorum') is False:
   (OUT/'two-down-health.json').write_text(json.dumps(state,indent=2)+'\n');break
  time.sleep(1)
 else: raise RuntimeError('two stopped nodes did not remove write quorum')
 go('reject-without-quorum' ,'^TestRustFSClusterNoWriteQuorum$','reject',timeout=30)
 run('recover',base+['start','rustfs1','rustfs2'],timeout=60)
 wait_ready('ready-recovered',env['RUSTFS_TEST_ENDPOINT'])
 go('read-after-recovery','^TestRustFSPersistence$','read')
 go('read-degraded-writes-after-recovery','^TestRustFSPersistence$','read-degraded')
 go('verify-rejected-write','^TestRustFSClusterRejectedWriteAbsent$','recovered')
 result['status']='PASS'
except Exception as e:
 result['status']='FAIL';result['error']=str(e);print(str(e),flush=True)
finally:
 try:run('logs',base+['logs','--no-color','--tail','150'],timeout=15)
 except Exception:pass
 try:run('runtime',base+['ps','-a','--format','json'],timeout=10)
 except Exception:pass
 try:run('cleanup',base+['down','--volumes'],timeout=45)
 except Exception as e:result['cleanup_error']=str(e)
 result['finished_at']=datetime.datetime.now(datetime.timezone.utc).isoformat()
 (OUT/'results.json').write_text(json.dumps(result,ensure_ascii=False,indent=2)+'\n')
 print('Result:',result['status'],'Evidence:',OUT,flush=True)
sys.exit(0 if result['status']=='PASS' else 1)
