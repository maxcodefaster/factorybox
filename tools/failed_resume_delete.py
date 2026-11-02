"""Native Stop proof for a failed writable-mapping restore, never a restore retry."""
import json
import time


INVENTORY = r'''
import hashlib,json,os,pathlib,stat,subprocess,sys
status=json.loads(subprocess.check_output(['/opt/factorybox/factorybox','worker-status']))
root=pathlib.Path(status['lifecycle']['root'])
record=json.loads((root/'objects'/sandbox_id/'record.json').read_bytes())
snapshot=record['pause']['snapshot_id']
capture=root/'objects'/sandbox_id/'captures'/snapshot
resume=root/'objects'/sandbox_id/'resumes'/snapshot
def digest(path):
 fd=os.open(path,os.O_RDONLY|os.O_NOFOLLOW|os.O_NOATIME)
 result=hashlib.sha256()
 with os.fdopen(fd,'rb') as stream:
  for data in iter(lambda:stream.read(1<<20),b''):result.update(data)
 return result.hexdigest()
checkpoint=json.loads((capture/'checkpoint.json').read_bytes())
files={str(capture/item['name']):digest(capture/item['name']) for item in checkpoint['files']}
assert all(files[str(capture/item['name'])]==item['sha256'] for item in checkpoint['files'])
files[str(capture/'checkpoint.json')]=digest(capture/'checkpoint.json')
for path in resume.rglob('*'):
 if stat.S_ISREG(path.lstat().st_mode):files[str(path)]=digest(path)
source={}
for path in pathlib.Path('/factory-source/project').rglob('*'):
 info=path.lstat()
 if stat.S_ISREG(info.st_mode):
  source[str(path)]={'sha256':digest(path),'device':info.st_dev,'inode':info.st_ino,
   'mode':stat.S_IMODE(info.st_mode),'uid':info.st_uid,'gid':info.st_gid}
print(json.dumps({'record':record,'files':files,'source':source,
 'writable_mapping_eacces':'mapping with 0xfffffffffffffff3' in (resume/'images/restore.log').read_text(),
 'checkpoint_files_verified':len(checkpoint['files'])}))
'''


def settle(run, ctl, sandbox_id, identity):
    observed = json.loads(run([*ctl, "state", sandbox_id]))
    record, runtime = observed["record"], observed.get("runtime") or {}
    operation = (record.get("pause") or {}).get("resume") or {}
    if (record["phase"] != "resuming" or not operation.get("attempted") or
            operation.get("completed_at") or not operation.get("error") or
            runtime.get("status") != "stopped" or runtime.get("pid") != 0):
        raise ValueError("Stop proof requires a genuinely attempted failed native restore")

    def inventory():
        return json.loads(run(["exec", identity, "python3", "-c",
                               "sandbox_id=" + repr(sandbox_id) + "\n" + INVENTORY]))

    before = inventory()
    if not before["writable_mapping_eacces"]:
        raise ValueError("negative control did not reach the original writable-mapping EACCES")
    started = time.monotonic()
    deleted = json.loads(run([*ctl, "delete", sandbox_id], timeout=60))
    seconds = time.monotonic() - started
    if (deleted["record"]["phase"] != "deleted" or deleted.get("runtime") is not None or
            not deleted["record"].get("delete", {}).get("completed_at")):
        raise ValueError("Stop did not confirm exact failed-runtime deletion")
    replay = json.loads(run([*ctl, "delete", sandbox_id], timeout=60))
    after = inventory()
    original = dict(after["record"])
    original["phase"], original["delete"] = before["record"]["phase"], before["record"].get("delete")
    if "delete" not in before["record"]:
        original.pop("delete")
    if original != before["record"]:
        raise ValueError("Stop changed original failed Resume authority or history")
    if any(after["files"].get(name) != digest for name, digest in before["files"].items()):
        raise ValueError("Stop changed original checkpoint, prepared inputs or failure evidence")
    if after["source"] != before["source"]:
        raise ValueError("Stop changed independently owned Source")
    if replay != deleted:
        raise ValueError("Stop replay changed its confirmed original outcome")
    return {
        "passed": True, "original_resume_preserved": True,
        "complete_checkpoint_and_failure_evidence_preserved": True,
        "canonical_source_preserved": True, "exact_delete_replay": True,
        "checkpoint_files_verified": before["checkpoint_files_verified"],
        "original_writable_mapping_eacces_confirmed": True,
        "delete_seconds": seconds,
    }
