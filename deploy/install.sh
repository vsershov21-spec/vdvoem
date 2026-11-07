#!/bin/bash
# One-time install of «Вдвоём» on the Studio VPS. Run as root:  sudo bash install.sh
# Creates the vdvoem user and service, auto-update and backup timers, and the Caddy site.
# The bot token is asked interactively and stored only in /etc/vdvoem/vdvoem.env (root:vdvoem 0640).
set -euo pipefail
[ "$(id -u)" = 0 ] || { echo "run as root: sudo bash $0"; exit 1; }
DOMAIN=${DOMAIN:-vdvoem.vershov.com}
RAW=https://raw.githubusercontent.com/vsershov21-spec/vdvoem/main/deploy

id vdvoem >/dev/null 2>&1 || useradd --system --home /var/lib/vdvoem --shell /usr/sbin/nologin vdvoem
install -d -o vdvoem -g vdvoem -m 0700 /var/lib/vdvoem /var/lib/vdvoem/backups
install -d -m 0755 /opt/vdvoem /opt/vdvoem/releases
install -d -m 0750 -g vdvoem /etc/vdvoem

for f in vdvoem.service update.sh backup.sh; do curl -fsSL "$RAW/$f" -o "/opt/vdvoem/$f"; done
chmod 755 /opt/vdvoem/update.sh /opt/vdvoem/backup.sh
mv /opt/vdvoem/vdvoem.service /etc/systemd/system/vdvoem.service

if [ ! -f /etc/vdvoem/vdvoem.env ]; then
  read -rsp "Токен бота от @BotFather: " TOKEN; echo
  [ -n "$TOKEN" ] || { echo "пустой токен"; exit 1; }
  umask 027
  cat > /etc/vdvoem/vdvoem.env <<ENV
BOT_TOKEN=$TOKEN
PUBLIC_URL=https://$DOMAIN
WEBHOOK_SECRET=$(head -c 24 /dev/urandom | od -An -tx1 | tr -d ' \n')
# AI (optional, OpenAI-compatible). Empty = rules only.
AI_URL=
AI_KEY=
AI_MODEL=
ENV
  chgrp vdvoem /etc/vdvoem/vdvoem.env && chmod 0640 /etc/vdvoem/vdvoem.env
fi

cat > /etc/systemd/system/vdvoem-update.service <<UNIT
[Unit]
Description=Вдвоём — pull latest release
After=network-online.target
[Service]
Type=oneshot
ExecStart=/opt/vdvoem/update.sh
UNIT
cat > /etc/systemd/system/vdvoem-update.timer <<UNIT
[Unit]
Description=Вдвоём — check for a new release every 5 minutes
[Timer]
OnBootSec=1min
OnUnitActiveSec=5min
[Install]
WantedBy=timers.target
UNIT
cat > /etc/systemd/system/vdvoem-backup.service <<UNIT
[Unit]
Description=Вдвоём — database backup
[Service]
Type=oneshot
User=vdvoem
Environment=DB_PATH=/var/lib/vdvoem/vdvoem.db
ExecStart=/opt/vdvoem/backup.sh
UNIT
cat > /etc/systemd/system/vdvoem-backup.timer <<UNIT
[Unit]
Description=Вдвоём — daily backup
[Timer]
OnCalendar=*-*-* 04:20
Persistent=true
[Install]
WantedBy=timers.target
UNIT

# Caddy site (kept in the Caddyfile so it survives restarts)
if ! grep -q "^$DOMAIN" /etc/caddy/Caddyfile; then
  cp /etc/caddy/Caddyfile "/etc/caddy/Caddyfile.bak.$(date +%s)"
  cat >> /etc/caddy/Caddyfile <<CADDY

$DOMAIN {
	encode zstd gzip
	reverse_proxy 127.0.0.1:8099
}
CADDY
  caddy validate --config /etc/caddy/Caddyfile --adapter caddyfile
  systemctl reload caddy
fi

systemctl daemon-reload
systemctl enable vdvoem.service vdvoem-update.timer vdvoem-backup.timer >/dev/null
/opt/vdvoem/update.sh || true
systemctl start vdvoem-update.timer vdvoem-backup.timer
systemctl --no-pager --lines=5 status vdvoem || true
echo
echo "Готово. Проверка: curl -s http://127.0.0.1:8099/healthz"
echo "Логи: journalctl -u vdvoem -f"
