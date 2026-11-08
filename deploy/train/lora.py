# LoRA fine-tune of Qwen2.5-3B-Instruct on the assistant's tool-call examples (Unsloth, one T4 GPU, ~1 h).
# usage: python lora.py train.jsonl  →  vdvoem-qwen.gguf (q4_k_m) for Ollama
import json, sys
from unsloth import FastLanguageModel
from datasets import Dataset
from trl import SFTTrainer, SFTConfig

path = sys.argv[1] if len(sys.argv) > 1 else "train.jsonl"
rows = [json.loads(l) for l in open(path, encoding="utf-8") if l.strip()]
print("examples:", len(rows))

model, tok = FastLanguageModel.from_pretrained("unsloth/Qwen2.5-3B-Instruct", max_seq_length=4096, load_in_4bit=True)
model = FastLanguageModel.get_peft_model(model, r=16, lora_alpha=16, lora_dropout=0,
    target_modules=["q_proj", "k_proj", "v_proj", "o_proj", "gate_proj", "up_proj", "down_proj"])

def fmt(r):
    return {"text": tok.apply_chat_template(r["messages"], tokenize=False)}

ds = Dataset.from_list(rows).map(fmt)
trainer = SFTTrainer(model=model, tokenizer=tok, train_dataset=ds,
    args=SFTConfig(dataset_text_field="text", per_device_train_batch_size=2, gradient_accumulation_steps=4,
                   num_train_epochs=3, learning_rate=2e-4, logging_steps=10, output_dir="out", report_to="none"))
trainer.train()
model.save_pretrained_gguf("vdvoem-qwen", tok, quantization_method="q4_k_m")
print("done: vdvoem-qwen/unsloth.Q4_K_M.gguf → rename to vdvoem-qwen.gguf")
