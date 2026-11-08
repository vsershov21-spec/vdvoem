#!/bin/bash
# Бесплатная модель для помощника прямо на сервере (Ollama, только 127.0.0.1 — снаружи недоступна).
#   bash ai-local.sh            поставить Ollama + qwen2.5:3b, подключить к «Вдвоём», прогнать проверку качества
#   bash ai-local.sh compare    ещё и сравнить с llama3.2:3b и qwen2.5:1.5b — отчёт в /root/llm-eval.md
#   bash ai-local.sh off        отключить модель (помощник вернётся к правилам)
# Память занимается только пока модель отвечает (OLLAMA_KEEP_ALIVE=5m), на диске ~2 ГБ на модель.
set -euo pipefail
[ "$(id -u)" = 0 ] || { echo "запусти под root"; exit 1; }
MODEL=${MODEL:-qwen2.5:3b}
ENVF=/etc/vdvoem/vdvoem.env
BIN=/opt/vdvoem/current/vdvoem

setenv() { # KEY VALUE → in vdvoem.env (replace or append)
  if grep -q "^$1=" "$ENVF"; then sed -i "s|^$1=.*|$1=$2|" "$ENVF"; else echo "$1=$2" >> "$ENVF"; fi
}

if [ "${1:-}" = off ]; then
  sed -i '/^AI_URL=/d;/^AI_MODEL=/d;/^AI_KEY=/d;/^AI_TIMEOUT=/d' "$ENVF"
  systemctl restart vdvoem && echo "Модель отключена, помощник работает на правилах."
  exit 0
fi

if ! command -v ollama >/dev/null; then
  # the official install.sh and github.com/…/download links answer 404/504 from this server — take the release
  # through the GitHub API (works) and set the service up ourselves
  echo "Ставлю Ollama (≈1,5 ГБ, через API GitHub)…"
  ID=$(curl -fsS -m 30 https://api.github.com/repos/ollama/ollama/releases/latest | python3 -c "import sys,json; print([a['id'] for a in json.load(sys.stdin)['assets'] if a['name']=='ollama-linux-amd64.tar.zst'][0])")
  curl -fL --retry 3 -H 'Accept: application/octet-stream' -o /tmp/ollama.tar.zst "https://api.github.com/repos/ollama/ollama/releases/assets/$ID"
  tar --zstd -xf /tmp/ollama.tar.zst -C /usr/local && rm -f /tmp/ollama.tar.zst
  id ollama >/dev/null 2>&1 || useradd -r -s /bin/false -U -m -d /usr/share/ollama ollama
  cat > /etc/systemd/system/ollama.service <<'U'
[Unit]
Description=Ollama (local models for «Вдвоём»)
After=network-online.target
[Service]
ExecStart=/usr/local/bin/ollama serve
User=ollama
Group=ollama
Restart=always
RestartSec=3
Environment=HOME=/usr/share/ollama
[Install]
WantedBy=multi-user.target
U
fi
mkdir -p /etc/systemd/system/ollama.service.d
cat > /etc/systemd/system/ollama.service.d/vdvoem.conf <<'U'
[Service]
Environment=OLLAMA_HOST=127.0.0.1:11434
Environment=OLLAMA_KEEP_ALIVE=5m
Environment=OLLAMA_NUM_PARALLEL=1
Environment=OLLAMA_MAX_LOADED_MODELS=1
Environment=OLLAMA_CONTEXT_LENGTH=4096
MemoryMax=4G
U
systemctl daemon-reload
systemctl enable --now ollama >/dev/null 2>&1 || true
systemctl restart ollama
for i in $(seq 1 30); do curl -sf http://127.0.0.1:11434/api/tags >/dev/null && break; sleep 1; done

echo "Скачиваю $MODEL (около 2 ГБ, один раз)…"
ollama pull "$MODEL"

setenv AI_URL http://127.0.0.1:11434/v1
setenv AI_MODEL "$MODEL"
setenv AI_KEY ollama
setenv AI_TIMEOUT 90
systemctl restart vdvoem
sleep 3
curl -sf http://127.0.0.1:8099/healthz && echo

echo "Проверка качества (30 типовых вопросов)…"
set -a; . "$ENVF"; set +a
MODELS="$MODEL"
if [ "${1:-}" = compare ]; then
  for m in llama3.2:3b qwen2.5:1.5b; do ollama pull "$m"; MODELS="$MODELS $m"; done
fi
"$BIN" eval $MODELS | tee /root/llm-eval.md
echo
echo "Готово: помощник работает на $MODEL. Отчёт: /root/llm-eval.md"
free -m | head -2
