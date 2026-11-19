#!/bin/sh
set -e
cd "$(dirname "$0")/../.."

# Override the display to run a second client alongside the first.
export DISPLAY=${CLIENTCTL_DISPLAY:-:99} LIBGL_ALWAYS_SOFTWARE=1
Xvfb $DISPLAY -screen 0 854x480x24 -nolisten tcp &
trap "kill $!" EXIT
go run ./tools/reference client-run "$@"
