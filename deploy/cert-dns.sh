#!/bin/bash
# Let's Encrypt for vdvoem.vershov.com through DNS (no inbound connections needed — the hoster resets
# validation coming from abroad). Uses acme.sh + acme-dns: one CNAME in the reg.ru DNS, then renewals are automatic.
#
#   bash cert-dns.sh     1st run: prints the CNAME to add in ispmanager → «Управление DNS»
#   bash cert-dns.sh     2nd run (after adding it): issues the certificate, switches Caddy to it
set -euo pipefail
[ "$(id -u)" = 0 ] || { echo "run as root"; exit 1; }
DOMAIN=${DOMAIN:-vdvoem.vershov.com}
ACMEDNS=${ACMEDNS:-https://auth.acme-dns.io}
CREDS=/root/.vdvoem-acmedns
CERTS=/etc/caddy/certs/vdvoem
SH=/root/.acme.sh/acme.sh

if [ ! -x "$SH" ]; then
  echo "Ставлю acme.sh…"
  t=$(mktemp -d); curl -fsSL https://github.com/acmesh-official/acme.sh/archive/refs/heads/master.tar.gz | tar xz -C "$t"
  (cd "$t"/acme.sh-master && ./acme.sh --install --force --nocron --home /root/.acme.sh >/dev/null); rm -rf "$t"
fi
# renewals: a daily systemd timer (the server has no cron)
if [ ! -f /etc/systemd/system/acme-renew.timer ]; then
  cat > /etc/systemd/system/acme-renew.service <<'U'
[Unit]
Description=Renew Let's Encrypt certificates (acme.sh)
[Service]
Type=oneshot
ExecStart=/root/.acme.sh/acme.sh --cron --home /root/.acme.sh
U
  cat > /etc/systemd/system/acme-renew.timer <<'U'
[Unit]
Description=Daily acme.sh renewal check
[Timer]
OnCalendar=*-*-* 04:17
RandomizedDelaySec=1h
Persistent=true
[Install]
WantedBy=timers.target
U
  systemctl daemon-reload && systemctl enable --now acme-renew.timer >/dev/null 2>&1
fi

if [ ! -s "$CREDS" ]; then
  echo "Регистрирую DNS-проверку…"
  r=$(curl -fsS --max-time 20 -X POST "$ACMEDNS/register")
  python3 - "$r" "$CREDS" "$ACMEDNS" <<'PY'
import json, sys
j = json.loads(sys.argv[1])
open(sys.argv[2], "w").write("\n".join([
  "export ACMEDNS_BASE_URL=" + sys.argv[3],
  "export ACMEDNS_USERNAME=" + j["username"],
  "export ACMEDNS_PASSWORD=" + j["password"],
  "export ACMEDNS_SUBDOMAIN=" + j["subdomain"],
  "FULLDOMAIN=" + j["fulldomain"], ""]))
PY
  chmod 600 "$CREDS"
fi
# shellcheck disable=SC1090
. "$CREDS"
command -v dig >/dev/null || { apt-get update -qq && apt-get install -y -qq bind9-dnsutils >/dev/null; }

target=$(dig +short CNAME "_acme-challenge.$DOMAIN" @ns1.hosting.reg.ru 2>/dev/null | sed 's/\.$//' || true)
if [ "$target" != "$FULLDOMAIN" ]; then
  echo
  echo "Добавь в ispmanager → Доменные имена → vershov.com → Записи → Создать:"
  echo "    Имя:      _acme-challenge.vdvoem"
  echo "    Тип:      CNAME"
  echo "    Значение: $FULLDOMAIN."
  echo
  echo "Потом запусти ещё раз:  bash cert-dns.sh"
  [ -n "$target" ] && echo "(сейчас там: $target)"
  exit 0
fi
echo "CNAME на месте → выпускаю сертификат"

"$SH" --set-default-ca --server letsencrypt >/dev/null
"$SH" --issue --dns dns_acmedns -d "$DOMAIN" --keylength ec-256 --dnssleep 30 || [ $? = 2 ]  # 2 = already valid

install -d -m 0750 -o root -g caddy "$CERTS"
"$SH" --install-cert -d "$DOMAIN" --ecc \
  --fullchain-file "$CERTS/fullchain.pem" --key-file "$CERTS/key.pem" \
  --reloadcmd "chgrp caddy $CERTS/*.pem; chmod 0640 $CERTS/*.pem; systemctl reload caddy"

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
openssl x509 -in "$CERTS/fullchain.pem" -noout -issuer -enddate
curl -fs --max-time 8 --resolve "$DOMAIN:443:127.0.0.1" "https://$DOMAIN/healthz" >/dev/null \
  && echo "Готово: https://$DOMAIN работает, продление автоматическое (acme-renew.timer)" \
  || echo "Caddy перезагружен, но проверка не прошла — пришли вывод"
