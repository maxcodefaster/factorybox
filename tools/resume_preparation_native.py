"""Owned incomplete-copy fault for the delivered Resume preparation."""
import json


def exercise(*, run, cli, identity, status, snapshot, output, result):
    root = status["lifecycle"]["root"]
    relative = root + "/objects/budget-source/resumes/" + snapshot
    capture = root + "/objects/budget-source/captures/" + snapshot
    original = json.loads(run(["exec", identity, "python3", "-c", """
import hashlib,json,pathlib,sys
root=pathlib.Path(sys.argv[1])
print(json.dumps({'checkpoint_sha256':hashlib.sha256((root/'checkpoint.json').read_bytes()).hexdigest()}))
""", capture]))
    wrapper = """#!/bin/sh
target=$(/usr/bin/readlink "/proc/$$/fd/4") || exit 91
case "$target" in
  '%s'/images|'%s/'*/images)
    /usr/bin/python3 -c '
import json,os,pathlib,stat
source=pathlib.Path("/proc/self/fd/3");target=pathlib.Path("/proc/self/fd/4")
images=sorted((p for p in source.iterdir() if p.is_file()),key=lambda p:p.stat().st_size)
original=images[-1];info=original.stat()
assert info.st_size>8<<20
partial=target/original.name
fd=os.open(partial,os.O_WRONLY|os.O_CREAT|os.O_EXCL|os.O_NOFOLLOW,stat.S_IMODE(info.st_mode))
try:
 with original.open("rb") as stream:os.write(fd,stream.read(4<<20))
 os.fsync(fd)
finally:os.close(fd)
complete=target/"inventory.img";original_complete=source/"inventory.img"
data=original_complete.read_bytes()
fd=os.open(complete,os.O_WRONLY|os.O_CREAT|os.O_EXCL|os.O_NOFOLLOW,stat.S_IMODE(original_complete.stat().st_mode))
try:assert os.write(fd,data)==len(data);os.fsync(fd)
finally:os.close(fd)
owned=partial.stat()
directory=target.stat()
pathlib.Path("/output/resume-partial-copy.json").write_text(json.dumps({"directory":os.readlink("/proc/self/fd/4"),"directory_device":directory.st_dev,"directory_inode":directory.st_ino,"complete_image":complete.name,"complete_inode":complete.stat().st_ino,"image":original.name,"bytes":owned.st_size,"device":owned.st_dev,"inode":owned.st_ino}))
'
    exit 74
    ;;
esac
exec /usr/bin/cp "$@"
""" % (relative, relative)
    # This file is fixture-owned. The upstream /usr/bin/cp stays unchanged.
    run(["exec", identity, "python3", "-c",
         "import pathlib,sys;p=pathlib.Path('/usr/local/bin/cp');"
         "assert p.is_file() and not p.is_symlink();p.write_text(sys.argv[1]);p.chmod(0o700)",
         wrapper])
    try:
        cli("resume", "--snapshot-id", snapshot, "budget-source")
    except ValueError as error:
        if "copy private fork images" not in str(error):
            raise
    else:
        raise ValueError("owned incomplete Resume copy did not fail before native restore")
    fault = json.loads((output / "resume-partial-copy.json").read_text())
    if not fault["directory"].startswith(relative + "/") or fault["bytes"] != 4 << 20:
        raise ValueError("Resume fault escaped its exact original preparation")
    observed = cli("state", "budget-source")
    result["resume_copy_failure_observed"] = True
    result["resume_copy_was_unattempted"] = (
        observed["record"]["phase"] == "resuming" and
        observed["record"]["pause"]["snapshot_id"] == snapshot and
        observed["record"]["pause"]["resume"]["attempted"] is False and
        not observed["record"]["pause"]["resume"].get("completed_at"))
    request = observed["record"]["pause"]["resume"]["requested_at"]
    run(["exec", identity, "python3", "-c",
         "import pathlib;pathlib.Path('/usr/local/bin/cp').write_text('#!/bin/sh\\nexec /usr/bin/cp \"$@\"\\n')"])
    pressure = status["store"] + "/resume-preparation-pressure"
    budget = json.loads(run(["exec", identity, "python3", "-c", """
import ctypes,json,os,pathlib,sys
capture,relative,pressure=sys.argv[1:]
files=json.loads((pathlib.Path(capture)/'checkpoint.json').read_bytes())['files']
total=sum(p['bytes'] for p in files)
present=sum((pathlib.Path(relative)/p['name']).stat().st_size for p in files if (pathlib.Path(relative)/p['name']).exists())
remaining=total-present;floor=(64+128)<<20;target=floor+(remaining+total)//2
fs=os.statvfs(capture);allocation=fs.f_bavail*fs.f_frsize-target
assert allocation>0 and target<floor+total
fd=os.open(pressure,os.O_WRONLY|os.O_CREAT|os.O_EXCL|os.O_NOFOLLOW,0o600)
try:
 libc=ctypes.CDLL(None,use_errno=True)
 observations=set()
 while True:
  # Extent metadata need not charge exactly the requested file length.
  # Keep the original measured admission window on both ext4 and Btrfs.
  if allocation<os.fstat(fd).st_size:os.ftruncate(fd,allocation)
  os.posix_fallocate(fd,0,allocation);os.fsync(fd)
  if libc.syncfs(fd)!=0:raise OSError(ctypes.get_errno(),'sync exact owned pressure filesystem')
  fs=os.statvfs(capture);available=fs.f_bavail*fs.f_frsize
  if floor+remaining<=available<floor+total:break
  observation=(allocation,available)
  assert observation not in observations,'pressure allocation did not converge'
  observations.add(observation)
  allocation+=available-target
  assert allocation>0,'pressure calibration escaped its exact owned file'
 owned=os.fstat(fd)
finally:os.close(fd)
fs=os.statvfs(capture);available=fs.f_bavail*fs.f_frsize
assert floor+remaining<=available<floor+total
print(json.dumps({'device':owned.st_dev,'inode':owned.st_ino,'bytes':owned.st_size,
 'available_bytes':available,'remaining_bytes':remaining,'complete_bytes':total,'floor_bytes':floor}))
""", capture, relative, pressure]))
    result["resume_incremental_copy_budget"] = budget

    def refused(key):
        try:
            cli("resume", "--snapshot-id", snapshot, "budget-source")
        except ValueError:
            current = cli("state", "budget-source")
            if (current["record"]["pause"]["resume"]["attempted"] is not False or
                    current["record"]["pause"]["resume"]["requested_at"] != request):
                raise ValueError("refused copy changed native execution authority")
            result[key] = True
        else:
            raise ValueError("foreign or changed Resume preparation was accepted")

    partial = fault["directory"] + "/" + fault["image"]
    extra = fault["directory"] + "/foreign.img"
    run(["exec", identity, "python3", "-c",
         "import os,sys;fd=os.open(sys.argv[1],os.O_WRONLY|os.O_CREAT|os.O_EXCL|os.O_NOFOLLOW,0o600);os.write(fd,b'foreign');os.close(fd)", extra])
    refused("resume_foreign_entry_refused")
    run(["exec", identity, "python3", "-c",
         "import pathlib,sys;p=pathlib.Path(sys.argv[1]);assert p.read_bytes()==b'foreign';p.unlink()", extra])
    link = status["store"] + "/foreign-resume-image-link"
    run(["exec", identity, "ln", partial, link])
    refused("resume_foreign_link_refused")
    run(["exec", identity, "rm", link])
    run(["exec", identity, "python3", "-c", """
import os,pathlib,sys
p=pathlib.Path(sys.argv[1]);fd=os.open(p,os.O_RDWR|os.O_NOFOLLOW)
try:original=os.read(fd,1);os.pwrite(fd,bytes([original[0]^255]),0);os.fsync(fd)
finally:os.close(fd)
pathlib.Path('/output/resume-original-prefix-byte').write_bytes(original)
""", partial])
    refused("resume_changed_prefix_refused")
    run(["exec", identity, "python3", "-c", """
import os,pathlib,sys
fd=os.open(sys.argv[1],os.O_WRONLY|os.O_NOFOLLOW)
try:os.pwrite(fd,pathlib.Path('/output/resume-original-prefix-byte').read_bytes(),0);os.fsync(fd)
finally:os.close(fd)
""", partial])
    run(["exec", identity, "mount", "-t", "tmpfs", "-o", "size=1m,mode=0700",
         "tmpfs", fault["directory"]])
    refused("resume_foreign_mount_refused")
    run(["exec", identity, "umount", fault["directory"]])
    resumed = cli("resume", "--snapshot-id", snapshot, "budget-source")
    result["resume_partial_inode_preserved"] = json.loads(run(["exec", identity, "python3", "-c",
        "import json,pathlib,sys;p=pathlib.Path(sys.argv[1]);s=p.stat();expected=json.loads(sys.argv[2]);"
        "print(json.dumps(s.st_dev==expected['device'] and s.st_ino==expected['inode'] and s.st_size>expected['bytes']))",
        partial, json.dumps(fault)]))
    directory = json.loads(run(["exec", identity, "python3", "-c", """
import json,pathlib,stat,sys
before=json.loads(sys.argv[1]);source=pathlib.Path(sys.argv[2]).stat();target=pathlib.Path(before['directory']).stat()
print(json.dumps({'same_directory':(target.st_dev,target.st_ino)==(before['directory_device'],before['directory_inode']),
 'source_owner':[source.st_uid,source.st_gid],'target_owner':[target.st_uid,target.st_gid],
 'source_mode':stat.S_IMODE(source.st_mode),'target_mode':stat.S_IMODE(target.st_mode)}))
""", json.dumps(fault), capture + "/images"]))
    result["resume_image_directory_observation"] = directory
    # The pinned CRIU nested-namespace restore adds reader/search permissions.
    # Before dispatch the root metadata lane requires the original 0700 mode;
    # after this real restore require its exact 0755 outcome on the same inode.
    result["resume_image_directory_preserved"] = (
        directory["same_directory"] and directory["source_owner"] == directory["target_owner"] and
        directory["source_mode"] == 0o700 and directory["target_mode"] == 0o755)
    result["resume_copied_file_ownership_completed"] = json.loads(run(["exec", identity, "python3", "-c", """
import json,pathlib,sys
before=json.loads(sys.argv[1]);source=pathlib.Path(sys.argv[2]);target=pathlib.Path(before['directory'])
owned=True
for name in [before['image'],before['complete_image']]:
 a=(source/name).stat();b=(target/name).stat()
 owned=owned and (a.st_uid,a.st_gid)==(b.st_uid,b.st_gid)
print(json.dumps(owned and (target/before['complete_image']).stat().st_ino==before['complete_inode']))
""", json.dumps(fault), capture + "/images"]))
    result["resume_only_missing_bytes_required"] = True
    run(["exec", identity, "python3", "-c", """
import json,os,pathlib,sys
p=pathlib.Path(sys.argv[1]);expected=json.loads(sys.argv[2]);s=p.lstat()
assert (s.st_dev,s.st_ino,s.st_size)==(expected['device'],expected['inode'],expected['bytes'])
assert s.st_nlink==1 and s.st_uid==0
p.unlink()
""", pressure, json.dumps(budget)])
    result["resume_owned_pressure_released"] = True
    result["resume_request_preserved"] = (
        resumed["record"]["owner"] == observed["record"]["owner"] and
        resumed["record"]["runtime_id"] == observed["record"]["runtime_id"] and
        resumed["record"]["pause"]["resume"]["requested_at"] == request)
    result["resume_checkpoint_preserved"] = (
        run(["exec", identity, "sha256sum", capture + "/checkpoint.json"]).split()[0] ==
        original["checkpoint_sha256"])
    return resumed
