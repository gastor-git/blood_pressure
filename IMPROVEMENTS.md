# Улучшения перед production

Итоговый план из разбора кодовой базы. Порядок — от критичного к мелочам.
Верификация после каждого шага: `go build ./...` → `go vet ./...` → `gofmt -l .` → `make test` → `make lint`.

Статус: все пункты выполнены, верификация (`build/vet/gofmt/test/lint`) чистая.

## 1. Бэкап БД (критично) — ✅

Данные медицинские, механизма резервного копирования нет.

- `storage/sqlite/sqlite.go`: метод `BackupTo(ctx, path string) error` через `VACUUM INTO ?` (работает при WAL, копия с живого бота).
- `cli/backup.go` (новый): подкоманда `backup -out <path>` (по умолчанию `backup_<сегодня>.db`).
- `cli/cli.go`: `case "backup"`, метод `BackupTo` в интерфейс `store`; `cli/help.go`: строка в справке.
- `Makefile`: таргет `cli_backup`.
- Тесты: `sqlite_test.go` — `TestBackupTo` (запись → копия содержит данные, оригинал цел); `cli_test.go` — `BackupTo` в `fakeStore` + тест `runBackup`.
- Проверить, что go-sqlite3 связывает параметр в `VACUUM INTO`. — ✅ связывает (тесты зелёные).

## 2. Персист offset `getUpdates` (критично) — ✅

Сейчас offset в памяти — при рестарте сброс в 0 даёт дубли (повторное «120 80 70» → вопрос про перезапись) и потерю событий при падении между fetch и process.

- `storage/sqlite/migrations.go`: `migration5` — таблица `meta(key TEXT PRIMARY KEY, value TEXT)`.
- `storage/sqlite/sqlite.go`: `GetOffset(ctx) (int, error)` (нет строки → 0) и `SetOffset(ctx, offset int) error` — вне интерфейса `storage.Storage`, чтобы не ломать моки.
- `events/telegram/telegram.go`: локальный интерфейс `offsetStore{GetOffset;SetOffset}`, type-assert в `New`, загрузка offset лениво в первом `Fetch` (без `context.Background()` в конструкторе), запись после `p.offset = …+1` в `Fetch`.
- Тесты: `sqlite_test.go` — round-trip `GetOffset/SetOffset` и дефолт 0; `commands_test.go` — `TestFetch_PersistsAndRestoresOffset`.

## 3. Уважать `retry_after` при 429 (критично) — ✅

Сейчас при rate-limit Telegram консьюмер ждёт фиксированную 1с (busy-loop).

- `clients/telegram/types.go`: поле `RetryAfter` → метод `func (e *APIError) RetryAfter() int` (тесты используют только `Code`/`Description`).
- `consumer/event-consumer/event-consumer.go`: в ветке ошибки — `errors.As` на локальный интерфейс `{RetryAfter() int}`, задержка = `retry_after` (cap 60с), иначе `errRetryDelay`.
- `notifier/notifier.go`: `backoffPause` при ошибке отправки с `RetryAfter>0` — пауза перед следующим получателем.
- Тесты: `event-consumer_test.go` (новый) — table-test `retryDelay`; `notifier_test.go` — `retryAfterDelay` + `backoffPause` с отменённым ctx.

## 4. Права на каталог данных в Docker (deploy) — ✅

Bind-mount `./data/sqlite` при первом запуске создаётся root → `USER 1000:1000` не может писать.

- `docker-compose.yml`: one-shot сервис `db-init` (`busybox`, `user: 0:0`, `mkdir -p && chown -R 1000:1000 ./data/sqlite`), у app `depends_on: db-init: condition: service_completed_successfully`. `docker compose config` валиден.

## 5. Healthcheck контейнера (deploy) — ✅

`restart: unless-stopped` есть, но нет liveness-сигнала.

- `cli/cli.go` + `cli/help.go`: подкоманда `health` (открывает БД через `runCLI`-путь, без `TG_KEY`).
- `main.go`: `case "health"` в CLI-переключателе.
- `docker-compose.yml`: `healthcheck: test: ["CMD","./blood_pressure","health"]`, interval 60s, timeout 10s (учитывает `_busy_timeout=5000`), start_period 20s.
- Тест: `cli_test.go` — `TestRun_Health`/`TestRun_Health_Error`.

## 6. Конфигурация через env (мелочи) — ✅

- `main.go`: константы → переменные через `envOr("BP_DB_PATH", …)`, `envOr("BP_TG_HOST", …)`, `envIntOr("BP_BATCH_SIZE", 100)`.
- `.env.dist`, README (раздел «Конфигурация»). В compose дефолт совпадает с volume.

## 7. Версия в логах (мелочи) — ✅

- `main.go`: при старте `runtime/debug.ReadBuildInfo()` → `service started, version=…, db=…`.

## Документация (обязательно по AGENTS.md) — ✅

- `README.md` обновлён: структура (`main.go`, `cli/`, `sqlite`, миграции, Makefile, compose, `.env.dist`), команды (`backup`, `health`), разделы «Конфигурация» и «Healthcheck и устойчивость», конвенции (опциональный интерфейс `offsetStore`, `retry_after`).

## Верификация — ✅

`go build ./...` → `go vet ./...` → `gofmt -l .` → `make test` (с `-race`) → `make lint` (0 issues) — всё чисто. Ручные прогоны `go run . backup`, `go run . health`, `BP_DB_PATH=… go run . health` — работают. Коммит — только по явному запросу.
