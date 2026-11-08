#!/bin/sh
# Installs the fine-tuned adapter from the GitHub release «lora» into the local Ollama — no root needed:
# downloads it, builds «vdvoem-intent» = qwen2.5:3b + adapter, tests both models on requests neither was tuned on,
# and switches the assistant only if the new one is better and fast enough. Re-run any time; safe.
#   sh install-lora.sh            (from the server; the commander can run it)
set -eu
REPO=vsershov21-spec/vdvoem
OLLAMA=http://127.0.0.1:11434
APP=http://127.0.0.1:8099
BASE=${BASE:-qwen2.5:3b}
NEW=${NEW:-vdvoem-intent}
W=${W:-$HOME/lora}
mkdir -p "$W" && cd "$W"
id=$(curl -fsS -m 20 "https://api.github.com/repos/$REPO/releases/tags/lora" | python3 -c "import sys,json; print([a['id'] for a in json.load(sys.stdin)['assets'] if a['name']=='vdvoem-intent-lora.gguf'][0])")
curl -fsSL --retry 3 -m 600 -H 'Accept: application/octet-stream' -o adapter.gguf "https://api.github.com/repos/$REPO/releases/assets/$id"
d=$(sha256sum adapter.gguf | cut -d' ' -f1)
echo "adapter $(du -h adapter.gguf | cut -f1) sha256:$d"
curl -fsS -m 300 -X POST --data-binary @adapter.gguf "$OLLAMA/api/blobs/sha256:$d" >/dev/null
curl -fsS -m 300 "$OLLAMA/api/create" -d "{\"model\":\"$NEW\",\"from\":\"$BASE\",\"adapters\":{\"adapter.gguf\":\"sha256:$d\"},\"stream\":false}"
echo
export AI_URL=$OLLAMA/v1 AI_KEY=ollama AI_TIMEOUT=120
/opt/vdvoem/current/vdvoem eval "$NEW" > eval-new.txt 2>&1
[ -s eval-base.txt ] && grep -q SUMMARY eval-base.txt || /opt/vdvoem/current/vdvoem eval "$BASE" > eval-base.txt 2>&1
new=$(grep '^SUMMARY' eval-new.txt | cut -d' ' -f2-)
old=$(grep '^SUMMARY' eval-base.txt | cut -d' ' -f2-)
echo "было:  $old"; echo "стало: $new"
better=$(python3 -c "import json,sys; o=json.loads(sys.argv[1]); n=json.loads(sys.argv[2]); print(int(n['held']>=o['held']+0.03 and n['p90']<=6.5 and n['gold']>=o['gold']-0.02))" "$old" "$new")
if [ "$better" = 1 ]; then
  curl -fsS -X POST "$APP/agent/model" -d "{\"model\":\"$NEW\"}"; echo; echo "включено: $NEW"
else
  echo "оставляю как было — новая модель не лучше (см. $W/eval-new.txt)"
fi
