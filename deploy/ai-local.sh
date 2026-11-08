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
  echo "Ставлю Ollama…"
  curl -fsSL https://ollama.com/install.sh | sh
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
