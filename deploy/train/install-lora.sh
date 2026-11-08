#!/bin/sh
# Installs the fine-tuned model from the GitHub release «lora» into the local Ollama — no root needed:
# downloads vdvoem-intent-q4_k_m.gguf, builds «vdvoem-intent» with qwen2.5's chat template, switches the app to it
# (one model for everything: a CPU server keeps one loaded), tests it on requests it was never tuned on and switches
# back if it is not better than the base. Re-run any time.   sh install-lora.sh
set -eu
REPO=vsershov21-spec/vdvoem
OLLAMA=http://127.0.0.1:11434
APP=http://127.0.0.1:8099
BASE=${BASE:-qwen2.5:3b}
NEW=${NEW:-vdvoem-intent}
W=${W:-$HOME/lora}
mkdir -p "$W" && cd "$W"
id=$(curl -fsS -m 20 "https://api.github.com/repos/$REPO/releases/tags/lora" | python3 -c "import sys,json; print([a['id'] for a in json.load(sys.stdin)['assets'] if a['name']=='vdvoem-intent-q4_k_m.gguf'][0])")
curl -fsSL --retry 3 -m 1800 -H 'Accept: application/octet-stream' -o model.gguf "https://api.github.com/repos/$REPO/releases/assets/$id"
d=$(sha256sum model.gguf | cut -d' ' -f1); echo "model $(du -h model.gguf | cut -f1) sha256:$d"
curl -fsS -m 1800 -X POST -T model.gguf "$OLLAMA/api/blobs/sha256:$d" >/dev/null   # -T streams: the file is 2 GB
python3 - "$d" "$BASE" "$NEW" > create.json <<'PY'
import json, sys, urllib.request
show = json.load(urllib.request.urlopen(urllib.request.Request("http://127.0.0.1:11434/api/show", data=json.dumps({"model": sys.argv[2]}).encode())))
print(json.dumps({"model": sys.argv[3], "files": {"model.gguf": "sha256:" + sys.argv[1]}, "template": show["template"], "system": show.get("system", ""), "stream": False}))
PY
curl -fsS -m 900 "$OLLAMA/api/create" -d @create.json; echo
rm -f model.gguf
export AI_URL=$OLLAMA/v1 AI_KEY=ollama AI_TIMEOUT=120
grep -q '^SUMMARY' eval-base.txt 2>/dev/null || { curl -fsS -X POST "$APP/agent/model" -d '{"model":"default"}' >/dev/null; /opt/vdvoem/current/vdvoem eval "$BASE" > eval-base.txt 2>&1; }
curl -fsS -X POST "$APP/agent/model" -d "{\"model\":\"$NEW\"}" >/dev/null
/opt/vdvoem/current/vdvoem eval "$NEW" > eval-new.txt 2>&1
new=$(grep '^SUMMARY' eval-new.txt | cut -d' ' -f2-); old=$(grep '^SUMMARY' eval-base.txt | cut -d' ' -f2-)
echo "было:  $old"; echo "стало: $new"
if python3 -c "import json,sys; o=json.loads(sys.argv[1]); n=json.loads(sys.argv[2]); sys.exit(0 if n['held']>=o['held'] and n['p90']<=6.5 and n['gold']>=o['gold']-0.02 else 1)" "$old" "$new"; then
  echo "оставляю $NEW"
else
  curl -fsS -X POST "$APP/agent/model" -d '{"model":"default"}' >/dev/null; echo "вернул $BASE — новая модель не лучше (см. $W/eval-new.txt)"
fi
