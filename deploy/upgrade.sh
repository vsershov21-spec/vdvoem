#!/bin/bash
# Обновить сам механизм обновлений (надёжная загрузка релиза) и сразу подтянуть свежую версию.
#   curl -fsSLO https://raw.githubusercontent.com/vsershov21-spec/vdvoem/main/deploy/upgrade.sh && bash upgrade.sh
set -euo pipefail
[ "$(id -u)" = 0 ] || { echo "запусти под root"; exit 1; }
curl -fsSL -o /opt/vdvoem/update.sh.new https://raw.githubusercontent.com/vsershov21-spec/vdvoem/main/deploy/update.sh
sh -n /opt/vdvoem/update.sh.new && chmod 755 /opt/vdvoem/update.sh.new && mv /opt/vdvoem/update.sh.new /opt/vdvoem/update.sh
/opt/vdvoem/update.sh || true
echo "Версия: $(/opt/vdvoem/current/vdvoem version)"
curl -sf http://127.0.0.1:8099/healthz && echo
