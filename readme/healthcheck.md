# Healthcheck и устойчивость

- `docker-compose.yml` проверяет живучесть контейнера: `healthcheck` выполняет `./blood_pressure health` (открывает БД, работает без `TG_KEY`), при трёх неудачах подряд `restart: unless-stopped` перезапускает контейнер.
- One-shot сервис `db-init` перед стартом `app` создаёт каталог `data/sqlite` и назначает владельцем uid 1000 — иначе bind-mount, созданный Docker от root, был бы недоступен процессу `USER 1000:1000` из Dockerfile.
- Offset `getUpdates` персистится в БД (таблица `meta`): после рестарта не приходят дубли уже обработанных событий.
- При `429` от Telegram консьюмер и notifier ждут `retry_after` (потолок 60с) вместо фиксированной паузы 1с.

- Зависимости ставятся штатным `go mod download`; vendor-каталога нет.
- **`CGO_ENABLED=0` ломает и сборку, и тесты** — из-за `go-sqlite3`. В Dockerfile статическая линковка сделана через `-tags netgo -ldflags '-extldflags "-static"'`, а не отключением cgo.
- Линтер `golangci-lint` (`make lint`, конфиг — `.golangci.yml`) и CI в GitHub Actions (`.github/workflows/ci.yml`) добавлены; CI прогоняет `go build` / `go vet` / `gofmt` / `make test` и `golangci-lint` при каждом `push` и `pull_request`. Локальная установка линтера: `go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.12` (в CI версия берётся из `ci.yml`). Настройка CI на стороне GitHub описана в [readme/ci.md](ci.md).
- Проект на `go 1.26` (`go.mod`), в Dockerfile — `golang:1.26`; `go-sqlite3` — `v1.14.49`.
