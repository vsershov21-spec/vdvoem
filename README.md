# Вдвоём

Telegram-бот и Mini App для пары: общая копилка идей, «хочу / очень хочу», общее свободное время, три варианта, оба подтверждают — событие в календарь. Бот сам раз в неделю предлагает идею (по оценке или рандомом), помощник отвечает на вопросы о планах.

- Сценарии и вкладки: [docs/SCENARIOS.md](docs/SCENARIOS.md)
- Бэкенд: Go 1.24, SQLite (`modernc.org/sqlite`), только stdlib. `cmd/vdvoem`
- Mini App: React + Vite + framer-motion. `web/`
- E2E: Playwright + эмулятор Bot API (`cmd/tgemu`). `e2e/run.sh`

## CI/CD
Пуш в `main` → GitHub Actions: `go vet`, `go test`, сборка web, e2e E01–E22 (скриншоты и логи — в ветке `e2e-artifacts`) → релиз `latest`.
Сервер раз в 5 минут забирает релиз, проверяет sha256, переключает, проверяет `/healthz` и при сбое откатывается.

## Установка на сервер (один раз, под root)
```sh
ssh root@150.241.65.146   # именно на сервер, не на своём компьютере
bash -c "$(curl -fsSL https://raw.githubusercontent.com/vsershov21-spec/vdvoem/main/deploy/install.sh)"   # спросит токен бота
```
Нужна A-запись `vdvoem.vershov.com` на IP сервера. Логи: `journalctl -u vdvoem -f`.
Настройки — `/etc/vdvoem/vdvoem.env` (токен, `AI_URL`/`AI_KEY`/`AI_MODEL` для помощника; без них он работает на правилах).
