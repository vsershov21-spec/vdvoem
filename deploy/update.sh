#!/bin/sh
# Pulls the «latest» GitHub release when it changed, verifies it, switches atomically, checks health, rolls back on failure.
set -eu
REPO=vsershov21-spec/vdvoem
BASE=https://github.com/$REPO/releases/download/latest
DIR=/opt/vdvoem
TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT
cd "$TMP"
curl -fsSL --retry 3 -o rel.tgz "$BASE/vdvoem-linux-amd64.tar.gz"
curl -fsSL --retry 3 -o rel.sha "$BASE/vdvoem-linux-amd64.tar.gz.sha256"
[ "$(sha256sum rel.tgz | cut -d' ' -f1)" = "$(cut -d' ' -f1 rel.sha)" ] || { echo "checksum mismatch"; exit 1; }
mkdir x && tar -C x -xzf rel.tgz
NEW=$(cut -c1-12 x/VERSION)
CUR=$(cat "$DIR/current/VERSION" 2>/dev/null | cut -c1-12 || true)
[ "$NEW" = "$CUR" ] && exit 0
echo "updating $CUR → $NEW"
rm -rf "$DIR/releases/$NEW" && mkdir -p "$DIR/releases" && mv x "$DIR/releases/$NEW"
chmod 755 "$DIR/releases/$NEW" "$DIR/releases/$NEW/vdvoem"
PREV=$(readlink -f "$DIR/current" 2>/dev/null || true)
[ -f /var/lib/vdvoem/vdvoem.db ] && sudo -u vdvoem env DB_PATH=/var/lib/vdvoem/vdvoem.db "$DIR/releases/$NEW/vdvoem" backup "/var/lib/vdvoem/backups/pre-$NEW.db" >/dev/null 2>&1 || true
ln -sfn "$DIR/releases/$NEW" "$DIR/current.new" && mv -Tf "$DIR/current.new" "$DIR/current"
systemctl restart vdvoem
for i in $(seq 1 30); do curl -sf http://127.0.0.1:8099/healthz >/dev/null && { echo "healthy $NEW"; ls -1dt "$DIR"/releases/* | tail -n +6 | xargs -r rm -rf; exit 0; }; sleep 1; done
echo "unhealthy, rolling back to $PREV"
[ -n "$PREV" ] && ln -sfn "$PREV" "$DIR/current.new" && mv -Tf "$DIR/current.new" "$DIR/current" && systemctl restart vdvoem
exit 1
