#!/bin/bash
# Голос для помощника: локальный whisper.cpp на 127.0.0.1:8178 — запись не уходит с сервера.
#   bash stt.sh            поставить (бинарники из релиза «stt», модель с Hugging Face) и включить в «Вдвоём»
#   MODEL=large-v3-turbo-q5_0 bash stt.sh   точнее, но медленнее (по умолчанию small-q5_1)
#   bash stt.sh bench FILE  сколько секунд уходит на файл
#   bash stt.sh off         выключить голос
set -euo pipefail
[ "$(id -u)" = 0 ] || { echo "запусти под root"; exit 1; }
REPO=vsershov21-spec/vdvoem
MODEL=${MODEL:-small-q5_1}
DIR=/opt/stt
ENVF=/etc/vdvoem/vdvoem.env
PORT=8178

setenv() { if grep -q "^$1=" "$ENVF"; then sed -i "s|^$1=.*|$1=$2|" "$ENVF"; else echo "$1=$2" >> "$ENVF"; fi; }

case "${1:-}" in
off)
  sed -i '/^STT_URL=/d' "$ENVF"; systemctl disable --now vdvoem-stt 2>/dev/null || true; systemctl restart vdvoem
  echo "Голос выключен."; exit 0 ;;
bench)
  f=${2:?файл}; s=$(date +%s.%N)
  curl -fsS -F "file=@$f" -F language=ru -F response_format=json "http://127.0.0.1:$PORT/inference"; echo
  echo "$(echo "$(date +%s.%N) - $s" | bc) с"; exit 0 ;;
esac

TMP=$(mktemp -d); trap 'rm -rf "$TMP"' EXIT
get() { # asset → file, through the API (the plain download link answers 504 from here)
  id=$(curl -fsS -m 30 "https://api.github.com/repos/$REPO/releases/tags/stt" | python3 -c "import sys,json; print([a['id'] for a in json.load(sys.stdin)['assets'] if a['name']=='$1'][0])")
  curl -fsSL --retry 3 -m 600 -H 'Accept: application/octet-stream' -o "$2" "https://api.github.com/repos/$REPO/releases/assets/$id"
}
echo "Скачиваю whisper-server и ffmpeg…"
get stt-linux-amd64.tar.gz "$TMP/stt.tgz"; get stt-linux-amd64.tar.gz.sha256 "$TMP/stt.sha"
[ "$(sha256sum "$TMP/stt.tgz" | cut -d' ' -f1)" = "$(cut -d' ' -f1 "$TMP/stt.sha")" ] || { echo "checksum mismatch"; exit 1; }
mkdir -p "$DIR/models" && tar -C "$DIR" -xzf "$TMP/stt.tgz" && chmod 755 "$DIR/whisper-server" "$DIR/ffmpeg"
M="$DIR/models/ggml-$MODEL.bin"
if [ ! -s "$M" ]; then
  echo "Скачиваю модель $MODEL…"
  curl -fL --retry 3 -o "$M.part" "https://huggingface.co/ggerganov/whisper.cpp/resolve/main/ggml-$MODEL.bin" && mv "$M.part" "$M"
fi
id vdvoem-stt >/dev/null 2>&1 || useradd -r -s /bin/false -U -M -d "$DIR" vdvoem-stt
cat > /etc/systemd/system/vdvoem-stt.service <<U
[Unit]
Description=Вдвоём — распознавание голоса (whisper.cpp, только 127.0.0.1)
After=network.target
[Service]
User=vdvoem-stt
Group=vdvoem-stt
Environment=PATH=$DIR:/usr/bin:/bin
WorkingDirectory=$DIR
ExecStart=$DIR/whisper-server -m $M -l ru -t 4 --host 127.0.0.1 --port $PORT --convert --no-timestamps
Restart=always
RestartSec=3
PrivateTmp=true
ProtectSystem=strict
ProtectHome=true
ReadWritePaths=$DIR
NoNewPrivileges=true
MemoryMax=1500M
Nice=5
[Install]
WantedBy=multi-user.target
U
chown -R vdvoem-stt:vdvoem-stt "$DIR"
systemctl daemon-reload && systemctl enable --now vdvoem-stt >/dev/null 2>&1; systemctl restart vdvoem-stt
for i in $(seq 1 60); do curl -sf -o /dev/null "http://127.0.0.1:$PORT/" && break; sleep 1; done
curl -sf -o /dev/null "http://127.0.0.1:$PORT/" || { echo "whisper-server не поднялся:"; journalctl -u vdvoem-stt -n 30 --no-pager; exit 1; }
setenv STT_URL "http://127.0.0.1:$PORT"
systemctl restart vdvoem
echo "Готово: голос включён ($MODEL). Пришли боту голосовое — проверь."
