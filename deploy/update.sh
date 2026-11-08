#!/bin/sh
# Pulls the «latest» GitHub release when it changed, verifies it, switches atomically, checks health, rolls back on failure.
set -eu
REPO=vsershov21-spec/vdvoem
BASE=https://github.com/$REPO/releases/download/latest
DIR=/opt/vdvoem
TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT
cd "$TMP"
# assets through the API first (the github.com/…/download redirect sometimes answers 504 from here), then the plain URL
get() { # name → file
  id=$(curl -fsS --retry 2 -m 20 "https://api.github.com/repos/$REPO/releases/tags/latest" | python3 -c "import sys,json; print([a['id'] for a in json.load(sys.stdin)['assets'] if a['name']=='$1'][0])" 2>/dev/null || true)
  if [ -n "$id" ] && curl -fsSL --retry 3 -m 300 -H 'Accept: application/octet-stream' -o "$2" "https://api.github.com/repos/$REPO/releases/assets/$id"; then return 0; fi
  curl -fsSL --retry 3 -m 300 -o "$2" "$BASE/$1"
}
get vdvoem-linux-amd64.tar.gz rel.tgz
get vdvoem-linux-amd64.tar.gz.sha256 rel.sha
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
