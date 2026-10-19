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
for log in sysbox-mgr.log sysbox-fs.log sysbox-mgr-stdio.log sysbox-fs-stdio.log; do
  if test -f "$output/$log"; then tail -n 20 "$output/$log" >&2; fi
done
exit 1
