#!/usr/bin/env bash
set -euo pipefail
/trace --object /trace.o --output /output/busy-events.bin --ready /output/trace-ready.json --result /output/trace-result.json > /output/trace.stdout 2> /output/trace.stderr &
observer=$!
finish() {
  status=$?
  kill -TERM "$observer" 2>/dev/null || true
  if ! wait "$observer"; then status=1; fi
  find /output -maxdepth 1 -type f -exec chown --reference=/output -- {} +
  exit "$status"
}
trap finish EXIT
for attempt in $(seq 1 150); do
  test ! -s /output/trace-ready.json || break
  kill -0 "$observer"
  sleep 0.02
done
test -s /output/trace-ready.json
/usr/bin/python3 /control.py
