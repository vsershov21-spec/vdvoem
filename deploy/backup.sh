#!/bin/sh
# Daily database copy, 14 kept.
set -eu
D=/var/lib/vdvoem/backups
mkdir -p "$D"
/opt/vdvoem/current/vdvoem backup "$D/vdvoem-$(date +%Y%m%d-%H%M).db"
ls -1t "$D"/vdvoem-*.db | tail -n +15 | xargs -r rm -f
