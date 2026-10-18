#!/usr/bin/env bash
# Reads only the exact owned, stopped fixture's volume.
set -euo pipefail
test "${FACTORYBOX_CAPTURE_ARCHIVE:-}" = 1
paths=()
for base in /input/factorybox-lifecycle-*; do
  if ! test -d "$base"; then continue; fi
  for path in "$base"/controller/objects/*/captures \
              "$base"/bundle/lower "$base"/bundle/upper "$base"/bundle/config.json; do
    if test -e "$path"; then paths+=("${path#/input/}"); fi
  done
done
tar --numeric-owner -C /input -cpf /output/capture-source.tar --files-from=/dev/null -- "${paths[@]}"
printf 'source-paths=%s\n' "${#paths[@]}" > /output/capture-archive.txt
