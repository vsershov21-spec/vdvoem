#!/bin/bash
# The hoster blocks the TLS-ALPN ACME challenge (admin.vershov.com has it disabled too),
# so the certificate for vdvoem.vershov.com must be issued over HTTP-01 only. Run as root.
set -euo pipefail
F=/etc/caddy/Caddyfile
DOMAIN=${DOMAIN:-vdvoem.vershov.com}
cp "$F" "$F.bak.$(date +%s)"
python3 - "$F" "$DOMAIN" <<'PY'
import re, sys
f, d = sys.argv[1], sys.argv[2]
s = open(f).read()
block = d + " {\n\ttls {\n\t\tissuer acme {\n\t\t\tdisable_tlsalpn_challenge\n\t\t}\n\t}\n\tencode zstd gzip\n\treverse_proxy 127.0.0.1:8099\n}\n"
s, n = re.subn(r"(?m)^" + re.escape(d) + r" \{.*?^\}\n?", block, s, flags=re.S)
if n == 0:
    s = s.rstrip("\n") + "\n\n" + block
open(f, "w").write(s)
PY
caddy validate --config "$F" --adapter caddyfile >/dev/null
systemctl reload caddy
echo "Caddy перезагружен, жду сертификат…"
for i in $(seq 1 45); do
  if curl -fs --max-time 5 "https://$DOMAIN/healthz" >/dev/null; then echo "Готово: https://$DOMAIN работает"; exit 0; fi
  sleep 2
done
echo "Сертификат ещё не выпущен. Последние сообщения Caddy:"
journalctl -u caddy --since "5 min ago" --no-pager | grep -i "$DOMAIN" | tail -8
