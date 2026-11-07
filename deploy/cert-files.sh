#!/bin/bash
# Use a certificate issued elsewhere (e.g. Let's Encrypt in ispmanager at reg.ru) for vdvoem.vershov.com.
# Put the downloaded files into /root (certificate, key, optional chain), then run as root:  bash cert-files.sh
set -euo pipefail
[ "$(id -u)" = 0 ] || { echo "run as root"; exit 1; }
DOMAIN=${DOMAIN:-vdvoem.vershov.com}
DIR=${1:-/root}
CERTS=/etc/caddy/certs/vdvoem
shopt -s nullglob nocaseglob

# unpack an archive if that is what was downloaded
for z in "$DIR"/*.zip; do command -v unzip >/dev/null && unzip -oq "$z" -d "$DIR/cert-unpacked" || python3 -m zipfile -e "$z" "$DIR/cert-unpacked"; done
files=("$DIR"/*.crt "$DIR"/*.pem "$DIR"/*.cer "$DIR"/*.key "$DIR"/*.ca "$DIR"/cert-unpacked/*)
key="" ; cert="" ; chain=""
for f in "${files[@]}"; do
  [ -f "$f" ] || continue
  if grep -q "PRIVATE KEY" "$f"; then key="$f"; continue; fi
  if grep -q "BEGIN CERTIFICATE" "$f"; then
    if openssl x509 -in "$f" -noout -ext subjectAltName 2>/dev/null | grep -qE "DNS:($DOMAIN|\*\.vershov\.com)"; then cert="$f"; else chain="$chain $f"; fi
  fi
done
[ -n "$key" ] || { echo "Не нашёл ключ (файл с PRIVATE KEY) в $DIR"; exit 1; }
[ -n "$cert" ] || { echo "Не нашёл сертификат для $DOMAIN в $DIR"; exit 1; }
[ "$(openssl x509 -in "$cert" -noout -pubkey | openssl sha256)" = "$(openssl pkey -in "$key" -pubout | openssl sha256)" ] || { echo "Ключ не подходит к сертификату"; exit 1; }
echo "Сертификат: $cert"; openssl x509 -in "$cert" -noout -subject -enddate

install -d -m 0750 -o root -g caddy "$CERTS"
{ cat "$cert"; for c in $chain; do [ "$c" = "$cert" ] || cat "$c"; done; } > "$CERTS/fullchain.pem"
cp "$key" "$CERTS/key.pem"
chgrp caddy "$CERTS"/*.pem && chmod 0640 "$CERTS"/*.pem

F=/etc/caddy/Caddyfile
cp "$F" "$F.bak.$(date +%s)"
python3 - "$F" "$DOMAIN" "$CERTS" <<'PY'
import re, sys
f, d, c = sys.argv[1:4]
s = open(f).read()
block = f"{d} {{\n\ttls {c}/fullchain.pem {c}/key.pem\n\tencode zstd gzip\n\treverse_proxy 127.0.0.1:8099\n}}\n"
s, n = re.subn(r"(?m)^" + re.escape(d) + r" \{.*?^\}\n?", block, s, flags=re.S)
if n == 0:
    s = s.rstrip("\n") + "\n\n" + block
open(f, "w").write(s)
PY
caddy validate --config "$F" --adapter caddyfile >/dev/null
systemctl reload caddy
sleep 2
curl -fs --max-time 8 "https://$DOMAIN/healthz" >/dev/null && echo "Готово: https://$DOMAIN работает" || echo "Caddy перезагружен, но проверка не прошла — пришли вывод"
