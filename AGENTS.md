# AGENTS.md

## Обзор проекта

Telegram-бот для записи и просмотра показаний артериального давления. Пользователь отправляет сообщение вида `120 80 70` — бот сохраняет систолическое/диастолическое давление и пульс с привязкой к дате и части суток; `/show` выводит показания за сегодня.

- **Go 1.25** (`go.mod`), модуль `blood-pressure-bot`.
- Единственная внешняя зависимость — `github.com/mattn/go-sqlite3 v1.14.16`. Это **cgo-биндинг**, поэтому нужен `CGO_ENABLED=1` и `gcc`.
- Хранилище — SQLite, файл `data/sqlite/storage.db`, единственная таблица `blood_pressure`, создаётся в `Init()`.
- Транспорт — Telegram Bot API поверх `net/http`. **Используется long polling**: в `getUpdates` передаётся параметр `timeout` (`longPollTimeout = 25`), поэтому холостого `time.Sleep(1s)` в консьюмере больше нет. HTTP-клиент с `Timeout: 30s`.
- Деплой — Docker, двухстадийная сборка, финальный образ `scratch`.
- Бизнес-правило: одна запись на связку (дата, часть суток, пользователь). Части суток: утро — до 12, день — 12–18, вечер — после 18.

## Структура репозитория

| Путь | Назначение |
|---|---|
| `main.go` | Сборка зависимостей и старт. Путь к БД, хост API и `batchSize` — константы, **не env** |
| `clients/telegram/telegram.go` | HTTP-клиент Bot API: `Updates`, `SendMessage`, `doRequest`. Методы API — константы `getUpdatesMethod` / `sendMessageMethod` |
| `clients/telegram/types.go` | DTO ответов API (`Update`, `IncomingMessage`, `From`, `Chat`) + поля ошибок API (`ok`, `error_code`, `description`, `parameters.retry_after`), типизированная `APIError` и её билдер `toError` |
| `events/type.go` | Интерфейсы `Fetcher` / `Processor` (оба принимают `ctx context.Context`), тип `Event` и `Type` |
| `events/telegram/telegram.go` | `*Processor` — реализует **оба** интерфейса; интерфейс `Client` объявлен на стороне потребителя ради моков |
| `events/telegram/commands.go` | Роутинг команд, `savePressure`, `show`, `dayPart`, `isPressure`, `getPressures` |
| `events/telegram/messages.go` | **Все** тексты для пользователя, константы с префиксом `msg` |
| `consumer/consumer.go` | Интерфейс `Consumer` с сигнатурой `Start(ctx context.Context) error` |
| `consumer/event-consumer/` | Пакет `event_consumer`: цикл fetch → process, завершается по отмене `ctx`; `recover` вокруг обработки каждого события |
| `storage/storage.go` | Интерфейс `Storage` (`Save` / `Show` / `IsExists`), модель `Pressure`; sentinel-ошибок больше нет |
| `storage/sqlite/sqlite.go` | Реализация на SQLite. `New` настраивает пул (`SetMaxOpenConns(1)`) и DSN (`_busy_timeout`, WAL); есть `Close()`. `Show()` возвращает **уже отформатированную** строку для пользователя |
| `lib/e/e.go` | `Wrap` (nil-safe) / `WrapIfErr` — обёртки над `fmt.Errorf("%w")` |
| `lib/timeloc/timeloc.go` | Единая таймзона учёта (`Asia/Yekaterinburg`) и формат даты; `time.LoadLocation` — один раз при инициализации пакета |
| `events/telegram/commands_test.go`, `storage/sqlite/sqlite_test.go` | Единственные тестовые файлы |
| `Makefile` | Таргеты `dc_*` для docker compose и `test` |
| `Dockerfile`, `docker-compose.yml` | Сборка и запуск контейнера |
| `.env.dist` | Шаблон окружения (`TG_KEY`, `COMPOSE_PROJECT_NAME`); реальный `.env` в `.gitignore` |
| `data/sqlite/` | Каталог БД (`storage.db` + `-wal` / `-shm`), монтируется как volume; в git не попадает (`*.db*`) |
| `tests_plan.md` | Обоснование скоупа тестов и того, что осознанно не покрыто |
| `PLAN.md` | Рабочий промпт-задание, **не** описание состояния проекта |

## Команды сборки и разработки

```sh
go build ./...                                 # сборка
go vet ./...                                   # должен быть чистым
gofmt -l .                                     # должен выводить пустой список
make test                                      # go test ./... -count=1 -race -cover
go test ./events/telegram -run TestDayPart -v  # один тест
TG_KEY="token" go run .                        # запуск вне Docker
```

Docker:

```sh
make dc_build          # сборка образа
make dc_up             # up -d --remove-orphans
make dc_ps / dc_logs   # статус / логи
make dc_stop           # остановить
make dc_down           # ВНИМАНИЕ: сносит volume (-v) и образы (--rmi=all)
```

- Зависимости ставятся штатным `go mod download`; vendor-каталога нет.
- **`CGO_ENABLED=0` ломает и сборку, и тесты** — из-за `go-sqlite3`. В Dockerfile статическая линковка сделана через `-tags netgo -ldflags '-extldflags "-static"'`, а не отключением cgo.
- Линтера (`golangci-lint`) и CI в репозитории **нет**. Верификация перед завершением задачи: `go build ./...` → `go vet ./...` → `gofmt -l .` → `make test`.
- Локальный тулчейн может быть новее, чем `go 1.25` в `go.mod` — это нормально, версию в `go.mod` не поднимать без запроса.

## Стиль кода и конвенции

**Поток данных:** `event_consumer.Consumer` → `Fetcher.Fetch` (`*Processor` → `Client.Updates`) → `Processor.Process` → `doCmd` → `storage.Storage`.

- **Интерфейсы объявляются на стороне потребителя.** `events/telegram.Client` существует ради моков; `*clients/telegram.Client` удовлетворяет ему неявно. **Не заменять поле `tg` на конкретный тип** — это сломает тесты.
- `*Processor` реализует и `events.Fetcher`, и `events.Processor`, поэтому передаётся дважды: `event_consumer.New(eventsProcessor, eventsProcessor, batchSize)`.
- **Ошибки:** в `events/` и `clients/` — `lib/e` через `defer` в начале функции (`defer func() { err = e.WrapIfErr("...", err) }()`), для чего нужны именованные возвращаемые значения. В `storage/sqlite` — напрямую `fmt.Errorf("...: %w", err)`. Sentinel-ошибки объявляются в пакете-владельце и сравниваются через `errors.Is`.
- **Именование:** пакеты `events/telegram` и `clients/telegram` оба называются `telegram`, поэтому в `main.go` и тестах нужен алиас `tgClient`. Пакет в `consumer/event-consumer` называется `event_consumer` — с подчёркиванием.
- **Тексты для пользователя — только в `events/telegram/messages.go`**, константы с префиксом `msg`. Тесты сравнивают отправленное именно с этими константами, инлайнить строки нельзя.
- **Команды бота** — константы `ShowCmd` / `HelpCmd` / `StartCmd` в `commands.go`.
- `storage/sqlite.Show()` смешивает слои: возвращает готовую строку для пользователя. При изменении формата чинить и `sqlite_test.go`, и `commands_test.go`.
- SQL — только параметризованные запросы (`?`), конкатенация значений в запрос запрещена.
- **`context.Context` пробрасывается сверху вниз** до HTTP и SQL: `Consumer.Start(ctx)` → `Fetch(ctx, …)` / `Process(ctx, …)` → `doCmd(ctx, …)` → `Client.Updates/SendMessage(ctx, …)` и `*Context`-методы `*sql.DB`. `context.Background()` / `context.TODO()` в прод-коде вне `main` запрещены; в `storage/sqlite` — только `QueryContext` / `ExecContext` / `QueryRowContext`.
- **Ответ Telegram API проверяется дважды**: HTTP-код (`doRequest`) и поле `ok` (`Updates`). Возврат `(nil, nil)` при ошибке API недопустим — ошибка становится `*telegram.APIError` (`error_code` / `description` / `retry_after`).
- **Токен не попадает в текст ошибок.** `Client` хранит его в поле `token` и санитизирует любую ошибку (`sanitize` заменяет токен на `***`, сохраняя цепочку через `Unwrap`) перед возвратом. Наружу пробрасываются только санитизированные ошибки клиента.
- **Регулярные выражения — пакетные `var … = regexp.MustCompile(…)`** (`pressureRe`, `numberRe`), компиляция внутри функции запрещена.
- `msgError` в `messages.go` — при сбое БД пользователь получает ответ, а не тишину.
- **Таймзона и формат даты — только через `lib/timeloc`** (`timeloc.Now()`, `timeloc.DateFormat`), не хардкодить строки в бизнес-коде.
- Язык кода, комментариев, сообщений и коммитов — **русский**.
- Форматирование — `gofmt`, табы, без исключений.

## Правила тестирования

Два файла, только stdlib (`testing`, `reflect.DeepEqual`, table-driven), без testify. **Новые зависимости в `go.mod` не добавлять.**

| Файл | Что покрывает | Покрытие |
|---|---|---|
| `events/telegram/commands_test.go` | парсинг, `dayPart`, роутинг команд, дедупликация, `show` | 51.8% пакета |
| `storage/sqlite/sqlite_test.go` | `Init` / `Save` / `IsExists` / `Show` | 82.7% пакета |

Запуск: `make test` (включает `-race -cover -count=1`), точечно — `go test ./events/telegram -run TestDayPart -v`.

Ключевые правила:

- **Тесты фиксируют текущее, в том числе багованное, поведение**, а не желаемое:
  - `TestDayPart`: `00:00` → `"день"` из-за `hour > 0` в `dayPart` (`events/telegram/commands.go:122`). Это баг, но тест закрепляет его.
  - `TestIsPressure`: `999 999 999` валидно — диапазоны не проверяются.
  - Исправление любого из этих багов требует одновременной правки теста; молча «чинить» их нельзя.
- Моки (`mockClient`, `mockStorage`) живут в тестовом файле пакета, отдельного пакета моков нет.
- SQLite-тесты используют файл в `t.TempDir()`, а **не** `:memory:` — пул `*sql.DB` открывает несколько соединений, каждое со своей пустой in-memory БД (флаки).
- Хелпер `today(t)` в `sqlite_test.go` берёт дату в `Asia/Yekaterinburg`, как и `Show`. Смена таймзоны в проде ломает тест.
- Осознанно вне покрытия: `lib/e`, `lib/timeloc`, `consumer/event-consumer`, `clients/telegram` (разросся: таймауты, санитизация токена, разбор `error_code`/`ok`, long polling — но всё ещё тонкий HTTP-враппер поверх stdlib), `Fetch` / `Process` / `meta` / `event`. Обоснование — в `tests_plan.md`.
- Новые тесты пишутся в существующие файлы, в том же table-driven стиле.

## Ограничения и безопасность

**Категорически нельзя без явного запроса:**

- Добавлять зависимости в `go.mod` / `go.sum` — стек намеренно ограничен stdlib + `go-sqlite3`.
- Менять схему БД (`Init()` в `storage/sqlite/sqlite.go:111`) или трогать `data/sqlite/storage.db`. Механизма миграций нет — любое изменение схемы сломает существующие данные.
- Править `Dockerfile`, `docker-compose.yml`, `Makefile`.
- Запускать `make dc_down` — таргет содержит `-v --rmi=all` и **удаляет volume с БД**.
- «Чинить» задокументированные баги (`dayPart`, отсутствие валидации диапазонов) без одновременной правки тестов и согласования.
- Коммитить в обход верификации: `go build ./...` → `go vet ./...` → `gofmt -l .` → `make test` должны пройти до коммита.
- Делать коммиты, аменды и пуши без явной просьбы пользователя.

**Секреты:**

- `TG_KEY` читается только из окружения (`os.Getenv` в `main.go`, `mustToken()` делает `log.Fatal` при пустом токене). Хардкодить токен, логировать его или подставлять в сообщения об ошибках запрещено — он входит в `basePath` HTTP-клиента. Клиент санитизирует ошибки (`sanitize`), поэтому наружу **логировать можно только санитизированные ошибки клиента**; `err.Error()` от `*url.Error` напрямую не пробрасывать.
- `.env` и `*.db` в `.gitignore`; шаблон — `.env.dist`, в нём только плейсхолдеры. `docker compose` подхватывает `.env` автоматически.
- `.dockerignore` исключает `.env`, `data/`, `.git` и `*.md` — секреты и документация в образ не попадают.
- Не добавлять реальные токены и дампы БД в тесты и фикстуры.

## Известные ловушки

- **Таймзона учёта — единая, в `lib/timeloc`** (`Asia/Yekaterinburg`), загружается один раз при инициализации пакета. Запись (`commands.go`) и выборка (`sqlite.go`) берут её из `timeloc.Now()`; хардкодить строку таймзоны в бизнес-коде нельзя — снова разъедутся.
- **Ключ пользователя — изменяемый `username`, а `from.id` не парсится** (`clients/telegram/types.go`). Пользователи без `@username` получают общий ключ `("", date, day_part)` и видят чужие показания через `/show`; смена ника отвязывает историю. Исправление требует новой колонки `user_id` и миграции существующих данных (Этап 3, схема заморожена).
- **TOCTOU между `IsExists` и `Save`** (`events/telegram/commands.go`): нет транзакции, в схеме нет `UNIQUE (date, day_part, user_name)`. Дедупликация держится только на аппликативной проверке и однопоточности консьюмера. Устранение — тоже изменение схемы (Этап 3).
- **Показания давления и `username` пишутся в лог в plaintext** (`commands.go` — `log.Printf("got new command …")`), без уровней и ротации. Ротация настроена лишь на уровне docker (`logging` в compose).
- Offset обновлений живёт только в памяти (`Processor.offset`) и сдвигается **до** обработки (`events/telegram/telegram.go`): ошибка `Process` не ретраится → событие теряется безвозвратно; после рестарта возможна переобработка ещё не сдвинутых событий.
- Дедлайн запроса задаёт **только** `http.Client.Timeout` (30s); отдельного `context.WithTimeout` на запрос нет — отмена приходит лишь через `ctx` из `main` (SIGINT/SIGTERM).
- `Dockerfile` запускает процесс под непривилегированным `USER 1000:1000`; каталог `data/sqlite`, смонтированный с хоста, должен быть доступен этому uid на запись, иначе бот упадёт при открытии БД.
