#!/usr/bin/env python3
"""Read a closed consumer restore failure without mounting or changing its filesystem."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import tarfile
import uuid

import reference


def main():
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument('run')
    args=parser.parse_args()
    if not re.fullmatch('factorybox-factory-consumer-[a-f0-9]{32}',args.run):raise ValueError('Expected one exact consumer failure')
    run=reference.ROOT/'artifacts'/args.run
    original=json.loads((run/'result.json').read_text())
    if original.get('passed') is not False or original.get('cleanup') is not True or not original.get('pause',{}).get('record',{}).get('pause',{}).get('completed_at'):
        raise ValueError('Requires a closed failure after completed Pause')
    raw=run/'failed-volume-0.tar'
    encoding=json.loads((run/'failed-volume-0.tar.encoding.json').read_text())
    payload=raw.with_name(raw.name+'.zst')
    if not payload.exists():payload,_=reference.artifact_storage.verified_replacement(payload,encoding['encoded'],encoding['original'])
    if payload.is_symlink():raise ValueError('Failure archive is redirected')
    capacity=reference.artifact_storage.require_capacity(reference.ROOT,run_reserve=0)
    output=reference.ROOT/'artifacts'/('factorybox-consumer-restore-read-'+uuid.uuid4().hex);output.mkdir()
    shutil.copyfile(__file__,output/'runner.py')
    result={'passed':False,'source':args.run,'source_result_sha256':reference.sha(run/'result.json'),
            'purpose':'Read the exact CRIU restore diagnostics for the retained failed Resume; no native execution or filesystem mount',
            'image':str(output/'store.img'),'production_qualified':False}
    (output/'intent.json').write_text(json.dumps(result,indent=2)+'\n');print(output,flush=True)
    class Reader:
        def __init__(self,file):self.file=file;self.digest=hashlib.sha256();self.bytes=0
        def read(self,size=-1):
            data=self.file.read(size);self.digest.update(data);self.bytes+=len(data);return data
    zero=bytes(1024**2)
    try:
        with subprocess.Popen(['zstd','-q','-dc','--long=31','--',str(payload)],stdout=subprocess.PIPE) as decoder:
            reader=Reader(decoder.stdout)
            with tarfile.open(fileobj=reader,mode='r|') as tar:
                for member in tar:
                    if member.name!='./fixed-store/store.img':continue
                    if not member.isfile() or member.size!=8<<30:raise ValueError('Failure image differs from its bounded store size')
                    digest=hashlib.sha256();count=0
                    with tar.extractfile(member) as source,(output/'store.img').open('xb') as destination:
                        while data:=source.read(1024**2):
                            digest.update(data);count+=len(data)
                            if data==zero[:len(data)]:destination.seek(len(data),os.SEEK_CUR)
                            else:destination.write(data)
                            if count%(16<<20)==0:
                                allocated=os.fstat(destination.fileno()).st_blocks*512
                                if capacity['allocated_bytes']+allocated+(4<<20)>reference.artifact_storage.ARTIFACT_LIMIT or shutil.disk_usage(output).free<reference.artifact_storage.FREE_RESERVE:
                                    raise ValueError('Sparse diagnostic expansion would exceed evidence or host reserve')
                        destination.truncate(member.size);destination.flush();os.fsync(destination.fileno())
                    if count!=member.size:raise ValueError('Failure image stream is incomplete')
                    result['image_bytes']=count;result['image_sha256']=digest.hexdigest()
            while reader.read(1024**2):pass
            if decoder.wait(timeout=15)!=0 or reader.bytes!=encoding['original']['bytes'] or reader.digest.hexdigest()!=encoding['original']['sha256']:
                raise ValueError('Complete failure archive decode differs from its original receipt')
        with (output/'store.img').open('rb') as image:
            if hashlib.file_digest(image,'sha256').hexdigest()!=result['image_sha256']:raise ValueError('Sparse image reconstruction differs')
        (output/'store.img').chmod(0o400)
        result['passed']=True;result['whole_archive_verified']=True;result['sparse_image_verified']=True
    finally:
        (output/'result.json').write_text(json.dumps(result,indent=2)+'\n');print(json.dumps(result),flush=True)


if __name__=='__main__':main()
