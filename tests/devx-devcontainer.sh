#!/bin/sh
# Test-only stderr observation. Preserve the real CLI, arguments, stdout and exit.
exec /usr/local/bin/devcontainer "$@" 2>>/var/log/factorybox-devcontainer.log
