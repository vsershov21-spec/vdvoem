# Дообучение бесплатной модели помощника (LoRA)

Модель учится на ваших же разговорах с помощником: сохраняется вопрос и то, какой инструмент
модель вызвала. Пример считается хорошим, когда вы нажали предложенную кнопку. Плюс 30 «золотых»
примеров из набора проверки.

## 1. Выгрузить примеры (на сервере)
```
set -a; . /etc/vdvoem/vdvoem.env; set +a
DB_PATH=/var/lib/vdvoem/vdvoem.db /opt/vdvoem/current/vdvoem export-train /root/train.jsonl
```
Имеет смысл, когда примеров ≥ 200. Скачать: `scp root@<сервер>:/root/train.jsonl .`

## 2. Обучить (бесплатно, ~1 час, Kaggle или Colab с GPU T4)
Открыть новый ноутбук, включить GPU, загрузить `train.jsonl` и `lora.py`, выполнить:
```
!pip install -q unsloth
!python lora.py train.jsonl
```
На выходе `vdvoem-qwen.gguf` (~2 ГБ).

## 3. Подключить на сервере
```
scp vdvoem-qwen.gguf root@<сервер>:/root/
# шаблон чата и вызова инструментов берём у исходной модели — меняется только веса
ollama show qwen2.5:3b --modelfile | sed 's|^FROM .*|FROM /root/vdvoem-qwen.gguf|' > /root/Modelfile
ollama create vdvoem-qwen -f /root/Modelfile
/opt/vdvoem/current/vdvoem eval qwen2.5:3b vdvoem-qwen     # сравнить до и после
MODEL=vdvoem-qwen bash ai-local.sh                          # переключить, если стало лучше
```
