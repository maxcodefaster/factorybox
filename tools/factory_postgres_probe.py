#!/usr/bin/env python3
"""Run Factory's relevant controller checks against an owned native PostgreSQL."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import socket
import subprocess
import uuid

import reference


def main():
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--factory-repo',type=Path,required=True)
    args=parser.parse_args()
    repo=args.factory_repo.resolve()
    owner=str(uuid.uuid4());name='factory-test-postgres-'+owner
    output=reference.new_artifact('factorybox-factory-postgres-'+owner.replace('-',''))
    output.mkdir();(output/'runner.py').write_bytes(Path(__file__).read_bytes())
    with socket.socket() as sock:
        sock.bind(('127.0.0.1',0));port=sock.getsockname()[1]
    url=f'postgresql://factory:owned-test-password@127.0.0.1:{port}/factory'
    wrapper=output/'docker'
    wrapper.write_text('#!/bin/sh\nexec /usr/bin/sudo -n /usr/bin/docker --context default "$@"\n');wrapper.chmod(0o755)
    files=['packages/db/migrations/0018_factorybox_execution.sql','apps/factory/src/runtime/factorybox.ts',
           'apps/factory/src/runtime/controller.ts','apps/factory/src/runtime/reserve-store.ts',
           'test/integration/factorybox-lifecycle.test.ts','test/integration/execution-reserve.test.ts',
           'apps/factory/src/workspace/source-controller.ts','test/integration/workspace-source.test.ts',
           'scripts/testing/factorybox-integration.ts']
    result={'schema':1,'owner':owner,'passed':False,'cleanup':False,'source_sha256':{
        p:hashlib.sha256((repo/p).read_bytes()).hexdigest() for p in files},
        'fixture':'Real pinned PostgreSQL 16; controlled Kubernetes replies',
        'production_qualified':False,'kubernetes_qualified':False,'native_lifecycle_qualified':False}
    (output/'intent.json').write_text(json.dumps(result,indent=2)+'\n')
    attempted=False
    try:
        attempted=True
        identity=reference.docker(['run','--detach','--name',name,'--label','factory.test-postgres='+owner,
            '--label','com.factorybox.run='+owner,'--network=host','--user','999:999','--cap-drop=ALL',
            '--security-opt=no-new-privileges','--cpus=.5','--memory=512m','--pids-limit=128',
            '--tmpfs','/var/lib/postgresql/data:uid=999,gid=999,mode=0700',
            '--env','POSTGRES_USER=factory','--env','POSTGRES_PASSWORD=owned-test-password','--env','POSTGRES_DB=factory',
            'docker.io/library/postgres:16.14-bookworm@sha256:64154d0babcb1741988719e703419af0382b19953706149f9872fbd0f438efa8',
            'postgres','-c','wal_level=logical','-c','listen_addresses=127.0.0.1','-p',str(port)])
        result['container_id']=identity;print(output,flush=True)
        env=dict(os.environ,PATH=str(output)+':'+os.environ['PATH'],DATABASE_URL=url,
            FACTORY_TEST_POSTGRES=json.dumps({'owner':owner,'url':url}),
            FACTORY_FACTORYBOX_WORKER_IMAGE='registry.invalid/worker@sha256:'+'a'*64,
            FACTORY_FACTORYBOX_EXECUTABLE_SHA256='b'*64,FACTORY_FACTORYBOX_ARCHITECTURE='amd64',FACTORY_FACTORYBOX_NODE='owned-node',FACTORY_FACTORYBOX_KERNEL='6.12.0')
        with (output/'controllers.log').open('w') as log:
            done=subprocess.run(['bun','--no-env-file','scripts/testing/factorybox-integration.ts'],
                cwd=repo,env=env,stdout=log,stderr=subprocess.STDOUT,timeout=360)
        result['exit_code']=done.returncode;result['passed']=done.returncode==0
    except Exception as error:result['error']=str(error)
    finally:
        if attempted:
            facts=subprocess.run([*reference.DOCKER,'inspect',name],capture_output=True,text=True,timeout=10)
            if facts.returncode==0:
                value=json.loads(facts.stdout)[0]
                if value['Config']['Labels'].get('factory.test-postgres')!=owner or (result.get('container_id') and value['Id']!=result['container_id']):
                    raise ValueError('Owned PostgreSQL changed before cleanup')
                (output/'postgres.log').write_text(reference.docker(['logs',name]))
                reference.owned_delete('container',name,owner)
                result['cleanup']=True
            elif 'No such object' in facts.stderr:result['cleanup']=True
        result['inputs_unchanged']=all(hashlib.sha256((repo/p).read_bytes()).hexdigest()==sha for p,sha in result['source_sha256'].items())
        result['passed']=result['passed'] and result['cleanup'] and result['inputs_unchanged']
        (output/'result.json').write_text(json.dumps(result,indent=2)+'\n');print(json.dumps(result),flush=True)
    if not result['passed']:raise RuntimeError('Factory PostgreSQL proof failed: '+str(output))


if __name__=='__main__':main()
