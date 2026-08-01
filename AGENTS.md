# AGENTS.md

## Обзор проекта

Telegram-бот для записи и просмотра показаний артериального давления. Пользователь отправляет сообщение вида `120 80 70` — бот сохраняет систолическое/диастолическое давление и пульс с привязкой к дате и части суток; `/show` выводит показания за сегодня.

- **Go 1.25** (`go.mod`), модуль `blood-pressure-bot`.
- Единственная внешняя зависимость — `github.com/mattn/go-sqlite3 v1.14.16`. Это **cgo-биндинг**, поэтому нужен `CGO_ENABLED=1` и `gcc`.
- Хранилище — SQLite, файл `data/sqlite/storage.db`, единственная таблица `blood_pressure`, создаётся в `Init()`.
- Транспорт — Telegram Bot API поверх `net/http`. **Long polling не используется**: в `getUpdates` нет параметра `timeout`, вместо него `time.Sleep(1s)` при пустом ответе.
- Деплой — Docker, двухстадийная сборка, финальный образ `scratch`.
- Бизнес-правило: одна запись на связку (дата, часть суток, пользователь). Части суток: утро — до 12, день — 12–18, вечер — после 18.

## Структура репозитория

| Путь | Назначение |
|---|---|
| `main.go` | Сборка зависимостей и старт. Путь к БД, хост API и `batchSize` — константы, **не env** |
| `clients/telegram/telegram.go` | HTTP-клиент Bot API: `Updates`, `SendMessage`, `doRequest`. Методы API — константы `getUpdatesMethod` / `sendMessageMethod` |
| `clients/telegram/types.go` | DTO ответов API (`Update`, `IncomingMessage`, `From`, `Chat`) |
| `events/type.go` | Интерфейсы `Fetcher` / `Processor`, тип `Event` и `Type` |
| `events/telegram/telegram.go` | `*Processor` — реализует **оба** интерфейса; интерфейс `Client` объявлен на стороне потребителя ради моков |
| `events/telegram/commands.go` | Роутинг команд, `savePressure`, `show`, `dayPart`, `isPressure`, `getPressures` |
| `events/telegram/messages.go` | **Все** тексты для пользователя, константы с префиксом `msg` |
| `consumer/consumer.go` | Интерфейс `Consumer` |
| `consumer/event-consumer/` | Пакет `event_consumer`: бесконечный цикл fetch → process |
| `storage/storage.go` | Интерфейс `Storage`, модель `Pressure`, sentinel-ошибки |
| `storage/sqlite/sqlite.go` | Реализация на SQLite. `Show()` возвращает **уже отформатированную** строку для пользователя |
| `lib/e/e.go` | `Wrap` / `WrapIfErr` — обёртки над `fmt.Errorf("%w")` |
| `events/telegram/commands_test.go`, `storage/sqlite/sqlite_test.go` | Единственные тестовые файлы |
| `Makefile` | Таргеты `dc_*` для docker compose и `test` |
| `Dockerfile`, `docker-compose.yml` | Сборка и запуск контейнера |
| `.env.dist` | Шаблон окружения (`TG_KEY`, `COMPOSE_PROJECT_NAME`); реальный `.env` в `.gitignore` |
| `data/sqlite/storage.db` | Файл БД, монтируется как volume; в git не попадает (`*.db`) |
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
- Язык кода, комментариев, сообщений и коммитов — **русский**.
- Форматирование — `gofmt`, табы, без исключений.

## Правила тестирования

Два файла, только stdlib (`testing`, `reflect.DeepEqual`, table-driven), без testify. **Новые зависимости в `go.mod` не добавлять.**

| Файл | Что покрывает | Покрытие |
|---|---|---|
| `events/telegram/commands_test.go` | парсинг, `dayPart`, роутинг команд, дедупликация, `show` | 54.7% пакета |
| `storage/sqlite/sqlite_test.go` | `Init` / `Save` / `IsExists` / `Show` | 70.8% пакета |

Запуск: `make test` (включает `-race -cover -count=1`), точечно — `go test ./events/telegram -run TestDayPart -v`.

Ключевые правила:

- **Тесты фиксируют текущее, в том числе багованное, поведение**, а не желаемое:
  - `TestDayPart`: `00:00` → `"день"` из-за `hour > 0` в `dayPart` (`events/telegram/commands.go:116`). Это баг, но тест закрепляет его.
  - `TestIsPressure`: `999 999 999` валидно — диапазоны не проверяются.
  - Исправление любого из этих багов требует одновременной правки теста; молча «чинить» их нельзя.
- Моки (`mockClient`, `mockStorage`) живут в тестовом файле пакета, отдельного пакета моков нет.
- SQLite-тесты используют файл в `t.TempDir()`, а **не** `:memory:` — пул `*sql.DB` открывает несколько соединений, каждое со своей пустой in-memory БД (флаки).
- Хелпер `today(t)` в `sqlite_test.go` берёт дату в `Asia/Yekaterinburg`, как и `Show`. Смена таймзоны в проде ломает тест.
- Осознанно вне покрытия: `lib/e`, `consumer/event-consumer`, `clients/telegram`, `storage.Remove`, `Fetch` / `Process` / `meta` / `event`. Обоснование — в `tests_plan.md`.
- Новые тесты пишутся в существующие файлы, в том же table-driven стиле.

## Ограничения и безопасность

**Категорически нельзя без явного запроса:**

- Добавлять зависимости в `go.mod` / `go.sum` — стек намеренно ограничен stdlib + `go-sqlite3`.
- Менять схему БД (`Init()` в `storage/sqlite/sqlite.go:107`) или трогать `data/sqlite/storage.db`. Механизма миграций нет — любое изменение схемы сломает существующие данные.
- Править `Dockerfile`, `docker-compose.yml`, `Makefile`.
- Запускать `make dc_down` — таргет содержит `-v --rmi=all` и **удаляет volume с БД**.
- «Чинить» задокументированные баги (`dayPart`, отсутствие валидации диапазонов) без одновременной правки тестов и согласования.
- Коммитить в обход верификации: `go build ./...` → `go vet ./...` → `gofmt -l .` → `make test` должны пройти до коммита.
- Делать коммиты, аменды и пуши без явной просьбы пользователя.

**Секреты:**

- `TG_KEY` читается только из окружения (`os.Getenv` в `main.go`). Хардкодить токен, логировать его или подставлять в сообщения об ошибках запрещено — он входит в `basePath` HTTP-клиента, поэтому URL запросов **нельзя логировать целиком**.
- `.env` и `*.db` в `.gitignore`; шаблон — `.env.dist`, в нём только плейсхолдеры. `docker compose` подхватывает `.env` автоматически.
- `.dockerignore` исключает `.env`, `data/`, `.git` и `*.md` — секреты и документация в образ не попадают.
- Не добавлять реальные токены и дампы БД в тесты и фикстуры.

## Известные ловушки

- **Таймзона `Asia/Yekaterinburg` захардкожена в двух местах**: `events/telegram/commands.go:47` и `storage/sqlite/sqlite.go:45`. Менять только вместе, иначе запись и выборка разъедутся.
- Мёртвый код: `storage.ErrNoSavedPages` не используется; ветка `ErrNoSavedPressure` в `commands.go:90-95` недостижима, т.к. `db.Query` никогда не возвращает `sql.ErrNoRows` — работает только проверка `msg == ""`.
- `Consumer.Start()` — value receiver, `handleEvents` — pointer receiver.
- Offset обновлений живёт только в памяти (`Processor.offset`) → после рестарта возможна переобработка событий.
- `clients/telegram` использует `http.Client{}` **без таймаута**; везде `context.Background()` / `context.TODO()`, дедлайнов нет.
- В `Consumer.Start()` ошибка `Fetch` приводит к `continue` без задержки — при недоступном API получается busy-loop с логированием.
- `savePressure` обращается к `pressures[0..2]` без проверки длины; безопасно только потому, что вызывается после `isPressure`.
- `docker-compose.yml` монтирует **файл** `./data/sqlite/storage.db`. Если файла нет, Docker создаст на его месте каталог и бот упадёт — файл должен существовать до `make dc_up`.
- Graceful shutdown отсутствует: `Start()` — бесконечный цикл, сигналы не обрабатываются.
