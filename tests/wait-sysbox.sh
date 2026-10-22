#!/bin/bash
# Wait only for the two owned fixture children and their private sockets.
set -euo pipefail
manager=$1
filesystem=$2
output=$3
socket_root=${4:-/run/sysbox}
budget=${5:-60}
[[ "$manager" =~ ^[0-9]+$ && "$filesystem" =~ ^[0-9]+$ && "$budget" =~ ^[0-9]+$ ]]
test "$manager" -gt 1
test "$filesystem" -gt 1
test "$budget" -ge 1
test "$budget" -le 60
test -d "$output"
started=$SECONDS
deadline=$((started + budget))
write_result() {
  manager_socket=false
  filesystem_socket=false
  if test -S "$socket_root/sysmgr.sock"; then manager_socket=true; fi
  if test -S "$socket_root/sysfs-seccomp.sock"; then filesystem_socket=true; fi
  printf '{"passed":%s,"seconds":%s,"deadline_seconds":%s,"manager_socket":%s,"filesystem_socket":%s}\n' \
    "$1" "$((SECONDS - started))" "$budget" "$manager_socket" "$filesystem_socket" \
    > "$output/sysbox-startup.json"
}
while true; do
  if test -S "$socket_root/sysfs-seccomp.sock" && test -S "$socket_root/sysmgr.sock"; then
    write_result true
    exit 0
  fi
  if ! kill -0 "$manager" 2>/dev/null || ! kill -0 "$filesystem" 2>/dev/null; then
    printf 'Owned Sysbox daemon exited before both private sockets were ready.\n' >&2
    break
  fi
  if test "$SECONDS" -ge "$deadline"; then
    printf 'Owned Sysbox sockets were not ready within %s seconds.\n' "$budget" >&2
    break
  fi
  sleep 0.1
done
write_result false
ps -o pid,ppid,stat,wchan:32,args -p "$manager,$filesystem" \
  > "$output/sysbox-startup-processes.txt" 2>&1 || true
python3 - "$manager" "$filesystem" "$PPID" > "$output/sysbox-startup-tree.txt" 2>&1 <<'PY' || true
import pathlib, sys, time
pending = [int(value) for value in sys.argv[1:3]]
parent = int(sys.argv[3])
seen = set()
deadline = time.monotonic() + 2
threads = 0
while pending and len(seen) < 128 and threads < 128 and time.monotonic() < deadline:
    pid = pending.pop()
    if pid in seen:
        continue
    seen.add(pid)
    root = pathlib.Path('/proc') / str(pid)
    try:
        status = dict(line.split(':', 1) for line in (root / 'status').read_text().splitlines() if ':' in line)
        if pid in [int(value) for value in sys.argv[1:3]] and int(status['PPid']) != parent:
            print('Refusing unrelated startup process', pid)
            continue
        print('PROCESS', pid, (root / 'cmdline').read_bytes().replace(b'\0', b' '))
        for thread in (root / 'task').iterdir():
            if threads >= 128 or time.monotonic() >= deadline:
                break
            threads += 1
            for name in ['wchan', 'stack', 'children']:
                try:
                    print(str(thread / name), (thread / name).read_text()[:65536])
                except OSError as error:
                    print(type(error).__name__)
            pending.extend(int(value) for value in (thread / 'children').read_text().split())
    except OSError as error:
        print(type(error).__name__)
PY
for log in sysbox-mgr.log sysbox-fs.log sysbox-mgr-stdio.log sysbox-fs-stdio.log; do
  if test -f "$output/$log"; then tail -n 20 "$output/$log" >&2; fi
done
exit 1
