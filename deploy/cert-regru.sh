#!/bin/bash
# Certificate for vdvoem.vershov.com via DNS-01 at reg.ru (Let's Encrypt cannot reach this server from abroad).
# Run once as root. Asks for the reg.ru login and API password (kept in /root/.acme.sh only), issues the certificate,
# hands it to Caddy and renews it automatically (daily systemd timer).
set -euo pipefail
[ "$(id -u)" = 0 ] || { echo "run as root"; exit 1; }
DOMAIN=${DOMAIN:-vdvoem.vershov.com}
CERTS=/etc/caddy/certs/vdvoem
ACME=/root/.acme.sh/acme.sh

read -rp  "Логин reg.ru: " REGRU_API_Username < /dev/tty
read -rsp "Пароль для API reg.ru (альтернативный): " REGRU_API_Password < /dev/tty; echo
export REGRU_API_Username REGRU_API_Password

if [ ! -x "$ACME" ]; then
  echo "Ставлю acme.sh…"
  rm -rf /tmp/acme-src && git clone -q --depth 1 https://github.com/acmesh-official/acme.sh /tmp/acme-src  # with dnsapi/dns_regru.sh
  (cd /tmp/acme-src && ./acme.sh --install --nocron --home /root/.acme.sh >/dev/null)
fi
"$ACME" --set-default-ca --server letsencrypt >/dev/null
"$ACME" --register-account --server letsencrypt >/dev/null 2>&1 || true

echo "Выпускаю сертификат через DNS reg.ru (1–5 минут)…"
"$ACME" --issue --dns dns_regru -d "$DOMAIN" --dnssleep 90 || { rc=$?; [ $rc = 2 ] || exit $rc; }  # 2 = already valid

install -d -m 0750 -o root -g caddy "$CERTS"
"$ACME" --install-cert -d "$DOMAIN" \
  --key-file "$CERTS/key.pem" --fullchain-file "$CERTS/fullchain.pem" \
  --reloadcmd "chgrp caddy $CERTS/*.pem && chmod 0640 $CERTS/*.pem && systemctl reload caddy"

# Caddy: use these files for vdvoem instead of its own ACME
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

# renewal: acme.sh decides when (≈ every 60 days) and reloads Caddy itself
cat > /etc/systemd/system/vdvoem-cert.service <<UNIT
[Unit]
Description=Вдвоём — renew the certificate (DNS-01, reg.ru)
[Service]
Type=oneshot
ExecStart=$ACME --cron --home /root/.acme.sh
UNIT
cat > /etc/systemd/system/vdvoem-cert.timer <<'UNIT'
[Unit]
Description=Вдвоём — daily certificate check
[Timer]
OnCalendar=*-*-* 05:17
Persistent=true
[Install]
WantedBy=timers.target
UNIT
systemctl daemon-reload
systemctl enable --now vdvoem-cert.timer >/dev/null

sleep 2
if curl -fs --max-time 8 "https://$DOMAIN/healthz" >/dev/null; then echo "Готово: https://$DOMAIN работает, сертификат продлевается сам"
else echo "Сертификат установлен, но проверка не прошла — пришли вывод выше"; fi
