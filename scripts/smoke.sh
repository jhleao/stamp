#!/bin/sh
set -eu

bin/stamp help >/dev/null
bin/stamp tutorial >/dev/null
bin/stamp skill >/dev/null
echo "Stamp CLI smoke test passed."
