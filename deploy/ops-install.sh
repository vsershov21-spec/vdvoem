#!/bin/bash
# One-time, as root. A tiny allow-listed operator for the Studio VPS: the unprivileged commander drops a request
# file, a root systemd path unit runs ONLY the actions listed below and writes the output where the commander can read it.
# No shell, no arguments, no passwords — just these verbs.
set -euo pipefail
[ "$(id -u)" = 0 ] || { echo "run as root"; exit 1; }
Q=/var/lib/vdvoem-ops
install -d -m 0733 -o root -g root "$Q/queue"          # commander may create files, not list or read others
install -d -m 0755 -o root -g root "$Q/out"

cat > /usr/local/sbin/vdvoem-ops <<'SH'
#!/bin/bash
# runs allow-listed actions requested in /var/lib/vdvoem-ops/queue
Q=/var/lib/vdvoem-ops
shopt -s nullglob
for f in "$Q"/queue/*; do
  a=$(basename "$f"); rm -f "$f"
  out="$Q/out/$a.log"
  {
    echo "# $a $(date -Is)"
    case "$a" in
      caddy-restart)  systemctl restart caddy; sleep 3; systemctl is-active caddy ;;
      caddy-reload)   caddy validate --config /etc/caddy/Caddyfile --adapter caddyfile && systemctl reload caddy ;;
      caddy-log)      journalctl -u caddy -n 200 --no-pager ;;
      vdvoem-restart) systemctl restart vdvoem; sleep 2; systemctl is-active vdvoem ;;
      vdvoem-log)     journalctl -u vdvoem -n 200 --no-pager ;;
      vdvoem-status)  systemctl --no-pager status vdvoem vdvoem-update.timer vdvoem-backup.timer caddy | head -60 ;;
      vdvoem-update)  /opt/vdvoem/update.sh ;;
      vdvoem-setup)   systemctl restart vdvoem; sleep 3; journalctl -u vdvoem -n 30 --no-pager | grep -i setup ;;
      *)              echo "not allowed: $a" ;;
    esac
    echo "# exit $?"
  } > "$out" 2>&1
  chmod 0644 "$out"
done
SH
chmod 0755 /usr/local/sbin/vdvoem-ops

cat > /etc/systemd/system/vdvoem-ops.service <<'UNIT'
[Unit]
Description=Вдвоём — allow-listed operator actions
[Service]
Type=oneshot
ExecStart=/usr/local/sbin/vdvoem-ops
UNIT
cat > /etc/systemd/system/vdvoem-ops.path <<'UNIT'
[Unit]
Description=Вдвоём — watch operator requests
[Path]
DirectoryNotEmpty=/var/lib/vdvoem-ops/queue
[Install]
WantedBy=paths.target
UNIT
systemctl daemon-reload
systemctl enable --now vdvoem-ops.path
echo "Готово: оператор установлен. Разрешено: caddy-restart caddy-reload caddy-log vdvoem-restart vdvoem-log vdvoem-status vdvoem-update vdvoem-setup"
