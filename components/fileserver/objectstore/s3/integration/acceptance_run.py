#!/usr/bin/env python3
"""Local TLS and destructive source-volume recovery evidence, not production signoff."""
import datetime
import json
import os
from pathlib import Path
import secrets
import shutil
import ssl
import subprocess
import sys
import tempfile
import time
import urllib.request

HERE=Path(__file__).resolve().parent
OUT=HERE.parents[4]/'audit/rustfs/acceptance'
OUT.mkdir(parents=True,exist_ok=True)
name='gala-rustfs-accept-'+secrets.token_hex(4)
image=os.getenv('RUSTFS_IMAGE','rustfs/rustfs:1.0.3')
env=dict(os.environ,RUSTFS_ACCESS_KEY='gala'+secrets.token_hex(8),RUSTFS_SECRET_KEY=secrets.token_hex(24),RUSTFS_TEST_BUCKET=name,RUSTFS_ACCEPTANCE_IAM='1',GOWORK='off')
volumes=[name+'-data',name+'-config']
compose=['docker','compose','-p',name,'-f',str(HERE.parent/'deploy/compose.yaml')]
env.update(RUSTFS_DATA_VOLUME=volumes[0],RUSTFS_CONFIG_VOLUME=volumes[1],RUSTFS_TLS_SERVER_NAME='localhost',RUSTFS_PORT='0',RUSTFS_BIND_ADDRESS='127.0.0.1',RUSTFS_CPU_LIMIT='2.0',RUSTFS_MEMORY_LIMIT='1536m')
result={'started_at':datetime.datetime.now(datetime.timezone.utc).isoformat(),'scope':'local TLS and quiescent backup restore','stages':[],'production_accepted':False}

def run(stage,args,timeout=60,extra=None):
 print(stage,flush=True);started=time.monotonic()
 p=subprocess.run(args,cwd=HERE.parent,env=env|(extra or {}),capture_output=True,text=True,timeout=timeout)
 (OUT/(stage+'.txt')).write_text(p.stdout+p.stderr)
 result['stages'].append({'name':stage,'exit_code':p.returncode,'seconds':round(time.monotonic()-started,3)})
 if p.returncode:raise RuntimeError(stage+' failed; see '+str(OUT/(stage+'.txt')))
 return p.stdout

def start(stage):
 run(stage,compose+['up','-d'])
 address=run(stage+'-port',compose+['port','rustfs','9000'],timeout=10).strip()
 env['RUSTFS_TEST_ENDPOINT']='https://'+address
 ctx=ssl.create_default_context(cafile=env['RUSTFS_TEST_CA_FILE'])
 for i in range(60):
  try:
   with urllib.request.urlopen(env['RUSTFS_TEST_ENDPOINT']+'/health/ready',context=ctx,timeout=2) as response:
    if response.status==200:
     (OUT/(stage+'-health.json')).write_bytes(response.read());return
  except Exception:pass
  state=subprocess.run(['docker','inspect',name+'-rustfs-1','--format','{{.State.Status}}'],capture_output=True,text=True,timeout=5)
  if state.stdout.strip()=='exited':raise RuntimeError(stage+': container exited')
  time.sleep(1)
 raise RuntimeError(stage+': readiness timed out')

try:
 image=run('image',['docker','image','inspect',image,'--format','{{.Id}}'],timeout=10).strip();result['image_id']=image;env['RUSTFS_IMAGE']=image
 with tempfile.TemporaryDirectory(prefix='gala-rustfs-accept-') as tmp:
  tmp=Path(tmp);cert=tmp/'cert';cert.mkdir(mode=0o755);backup=tmp/'backup';backup.mkdir(mode=0o700)
  config=cert/'openssl.cnf'
  config.write_text('[req]\ndistinguished_name=dn\nx509_extensions=ext\nprompt=no\n[dn]\nCN=localhost\n[ext]\nsubjectAltName=DNS:localhost,IP:127.0.0.1\nbasicConstraints=critical,CA:TRUE\nkeyUsage=critical,digitalSignature,keyEncipherment,keyCertSign\nextendedKeyUsage=serverAuth\n')
  run('certificate',['openssl','req','-x509','-newkey','rsa:2048','-nodes','-days','1','-config',str(config),'-keyout',str(cert/'rustfs_key.pem'),'-out',str(cert/'rustfs_cert.pem')],timeout=20)
  # Only ephemeral fixture files are readable in the seed mount. The runtime
  # TLS volume gets UID 10001 ownership and a 0600 private key.
  (cert/'rustfs_key.pem').chmod(0o644)
  env['RUSTFS_TEST_CA_FILE']=str(cert/'rustfs_cert.pem');env['RUSTFS_BACKUP_DIR']=str(backup)
  for i,volume in enumerate(volumes):run('volume-'+str(i),['docker','volume','create',volume],timeout=10)
  run('seed-certificate',['docker','run','--rm','--user','0','--entrypoint','sh','-v',str(cert)+':/seed:ro','-v',volumes[1]+':/config','-e','RUSTFS_ACCESS_KEY','-e','RUSTFS_SECRET_KEY',image,'-c','mkdir -p /config/tls && cp /seed/rustfs_cert.pem /seed/rustfs_key.pem /config/tls/ && cp /seed/rustfs_cert.pem /config/tls/ca.pem && printf %s "$RUSTFS_ACCESS_KEY" > /config/access.key && printf %s "$RUSTFS_SECRET_KEY" > /config/secret.key && chown -R 10001:10001 /config && chmod 600 /config/access.key /config/secret.key /config/tls/rustfs_key.pem'],timeout=20)
  start('start-source')
  runtime=json.loads(run('runtime',['docker','inspect',name+'-rustfs-1','--format','{"user":{{json .Config.User}},"readonly_root":{{.HostConfig.ReadonlyRootfs}},"cap_drop":{{json .HostConfig.CapDrop}},"security_opt":{{json .HostConfig.SecurityOpt}},"pids_limit":{{.HostConfig.PidsLimit}},"mounts":{{json .Mounts}},"restart_policy":{{json .HostConfig.RestartPolicy}},"memory":{{.HostConfig.Memory}},"cpus":{{.HostConfig.NanoCpus}},"log_config":{{json .HostConfig.LogConfig}},"env":{{json .Config.Env}}}'],timeout=10))
  if runtime['user']!='10001:10001' or not runtime['readonly_root'] or 'ALL' not in runtime['cap_drop']:raise RuntimeError('hardening settings not applied')
  if not any(x.startswith('no-new-privileges') for x in runtime['security_opt']):raise RuntimeError('no-new-privileges missing')
  if any(x.startswith('RUSTFS_ACCESS_KEY=') or x.startswith('RUSTFS_SECRET_KEY=') for x in runtime['env']):raise RuntimeError('root credentials exposed as environment values')
  for attempt in range(20):
   health=run('health-status',['docker','inspect',name+'-rustfs-1','--format','{{.State.Health.Status}}'],timeout=5).strip()
   if health=='healthy':break
   time.sleep(1)
  else:raise RuntimeError('Docker TLS healthcheck did not become healthy')
  if runtime['memory']!=1536*(1<<20) or runtime['cpus']!=2_000_000_000 or runtime['pids_limit']!=256:raise RuntimeError('resource limits not applied')
  if runtime['log_config']!={'Type':'json-file','Config':{'max-file':'3','max-size':'10m'}}:raise RuntimeError('log rotation not applied')
  if runtime['restart_policy']['Name']!='unless-stopped':raise RuntimeError('restart policy not applied')
  mounts={m['Destination']:m['RW'] for m in runtime['mounts']}
  if mounts.get('/data') is not True or mounts.get('/etc/rustfs') is not False:raise RuntimeError('persistent/config mount modes incorrect')
  permissions=run('secret-permissions',['docker','exec',name+'-rustfs-1','stat','-c','%u:%g:%a','/etc/rustfs/access.key','/etc/rustfs/secret.key','/etc/rustfs/tls/rustfs_key.pem'],timeout=10).splitlines()
  if permissions!=['10001:10001:600']*3:raise RuntimeError('secret file ownership/mode incorrect')
  result['hardening_verified']=True
  run('tls-contract' ,['go','test','-race','-json','-count=1','-timeout=180s','-run','^TestRustFS(Contract|TLSTrust|TenantPolicy|PresignedRequests|RejectsWrongCredentials)$','.'],timeout=210)
  run('backup',['go','test','-race','-v','-count=1','-timeout=60s','-run','^TestRustFSBackupRestore$','.'],timeout=90,extra={'RUSTFS_ACCEPTANCE_PHASE':'backup'})
  shutil.copyfile(backup/'manifest.json',OUT/'backup-manifest.json')
  recovery_started=time.monotonic()
  run('destroy-source-container',compose+['down'],timeout=20)
  run('destroy-source-volume',['docker','volume','rm',volumes[0]],timeout=20)
  run('empty-replacement-volume',['docker','volume','create',volumes[0]],timeout=10)
  start('start-replacement')
  run('restore',['go','test','-race','-v','-count=1','-timeout=60s','-run','^TestRustFSBackupRestore$','.'],timeout=90,extra={'RUSTFS_ACCEPTANCE_PHASE':'restore'})
  result['local_recovery_seconds']=round(time.monotonic()-recovery_started,3)
  result['snapshot_objects']=3
  result['status']='PASS'
except Exception as e:
 result['status']='FAIL';result['error']=str(e);print(str(e),flush=True)
finally:
 try:run('server-logs',compose+['logs','--no-color'],timeout=10)
 except Exception:pass
 try:run('remove-container',compose+['down'],timeout=15)
 except Exception:pass
 for i,v in enumerate(volumes):
  try:run('remove-volume-'+str(i),['docker','volume','rm',v],timeout=15)
  except Exception:pass
 result['finished_at']=datetime.datetime.now(datetime.timezone.utc).isoformat()
 (OUT/'results.json').write_text(json.dumps(result,ensure_ascii=False,indent=2)+'\n')
 print('Result:',result['status'],'Production accepted:',result['production_accepted'],flush=True)
sys.exit(0 if result['status']=='PASS' else 1)
