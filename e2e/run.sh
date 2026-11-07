#!/bin/sh
# Full e2e: build, start the Bot API emulator and the app on a throwaway DB, run Playwright, stop everything.
set -eu
ROOT=$(cd "$(dirname "$0")/.." && pwd)
WORK=${WORK:-$ROOT/e2e/.run}
rm -rf "$WORK" && mkdir -p "$WORK" "$ROOT/e2e/shots"
cd "$ROOT"
go build -o "$WORK/vdvoem" ./cmd/vdvoem
go build -o "$WORK/tgemu" ./cmd/tgemu
(cd web && npm ci --no-audit --no-fund >/dev/null && npm run build >/dev/null)
TOKEN=e2e-$(od -An -N12 -tx1 /dev/urandom | tr -d ' \n')
"$WORK/tgemu" -listen 127.0.0.1:18098 > "$WORK/tgemu.log" 2>&1 &
EMU=$!
BOT_TOKEN=000:test PUBLIC_URL=http://localhost:18099 WEBHOOK_SECRET=e2e-secret LISTEN=127.0.0.1:18099 \
  DB_PATH="$WORK/e2e.db" WEB_DIR="$ROOT/web/dist" TG_API_BASE=http://127.0.0.1:18098 SETUP=1 DEV_TOKEN=$TOKEN \
  "$WORK/vdvoem" > "$WORK/app.log" 2>&1 &
APP=$!
trap 'kill $APP $EMU 2>/dev/null || true' EXIT
for i in $(seq 1 50); do curl -sf http://127.0.0.1:18099/healthz >/dev/null && break; sleep 0.2; done
cd e2e
[ -d node_modules ] || npm i --no-audit --no-fund >/dev/null
BASE=http://localhost:18099 EMU=http://127.0.0.1:18098 DEV_TOKEN=$TOKEN npx playwright test "$@" || { tail -40 "$WORK/app.log"; exit 1; }
