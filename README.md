# Blood Pressure Bot

## Краткое описание проекта

Telegram-бот для записи и просмотра показаний артериального давления. Пользователь может ввести показания быстрым способом — сообщение вида `120 80 70` — или пошагово через `/add`: выбор даты (по умолчанию — сегодня), выбор части суток (Утро/День/Вечер) и ввод всех показаний одним сообщением вида `120 80 70` с валидацией диапазонов; `/show` выводит показания за сегодня, `/download` отправляет CSV-файл со всеми показаниями за всё время. При дубликате (показания уже есть за эту дату и часть суток) бот показывает старые и новые показания и спрашивает, перезаписать ли запись. Отдельный пакет `notifier` рассылает напоминания «Пора передать показания за {часть суток}» в 11:30 / 17:30 / 23:30 (таймзона `lib/timeloc`) пользователям, ещё не передавшим показания за текущую часть суток.

## Структура репозитория

| Путь | Назначение |
|---|---|
| `main.go` | Сборка зависимостей и старт. Путь к БД, хост API и `batchSize` — константы, **не env**. Рядом с консьюмером в отдельной горутине запускается `notifier` |
| `clients/telegram/telegram.go` | HTTP-клиент Bot API: `Updates`, `SendMessage`, `SendKeyboard`, `RemoveKeyboard`, `SendDocument`, `doRequest`. Методы API — константы `getUpdatesMethod` / `sendMessageMethod` / `sendDocumentMethod`. Общие хелперы `do` (выполнение запроса, проверка HTTP-статуса, `sanitize`) и `checkOK` (поле `ok` → `APIError`); multipart-тело `SendDocument` собирает `multipartBody` |
| `clients/telegram/types.go` | DTO ответов API (`Update`, `IncomingMessage`, `From`, `Chat`) + поля ошибок API (`ok`, `error_code`, `description`, `parameters.retry_after`), типизированная `APIError` и её билдер `toError`; клавиатуры `ReplyKeyboardMarkup` / `ReplyKeyboardRemove` |
| `events/type.go` | Интерфейсы `Fetcher` / `Processor` (оба принимают `ctx context.Context`), тип `Event` и `Type` |
| `events/telegram/telegram.go` | `*Processor` — реализует **оба** интерфейса; интерфейс `Client` объявлен на стороне потребителя ради моков |
| `events/telegram/commands.go` | Роутинг команд, `savePressure`, `save`, `confirmOverwrite`, `show`, `download`, `DayPart`, `isPressure`, `getPressures`. Вызов `storage.RegisterUser` в `doCmd` при каждом сообщении |
| `events/telegram/dialog.go` | State-machine пошагового ввода `/add` и подтверждения перезаписи: `session` по `user_id`, шаги дата → часть суток → показания одним сообщением (`120 80 70`) → при дубликате `stateConfirmOverwrite` (`handleOverwrite` → `doOverwrite`); reply-клавиатуры (`dateKeyboard`, `dayPartKeyboard`, `overwriteKeyboard`), `parseUserDate`, `dayPartKey` |
| `events/telegram/csv.go` | Генерация CSV-выгрузки `/download`: `formatCSV` (BOM + CRLF, разделитель `;`, фиксированный порядок утро→день→вечер, значения показаний как `систолическое/диастолическое/пульс`, пустые ячейки) и `csvFilename` |
| `events/telegram/messages.go` | **Все** тексты для пользователя, константы с префиксом `msg` (включая шапку CSV `msgCSVHeader`, промпты и кнопки диалога `/add` и подтверждения перезаписи: `msgDuplicatePrompt`, `msgOverwriteButton`, `msgKeepButton`, `msgOverwritten`, `msgKeepExisting`). `MsgReminder` — единственная экспортированная, нужна пакету `notifier` |
| `consumer/consumer.go` | Интерфейс `Consumer` с сигнатурой `Start(ctx context.Context) error` |
| `consumer/event-consumer/` | Пакет `event_consumer`: цикл fetch → process, завершается по отмене `ctx`; `recover` вокруг обработки каждого события |
| `notifier/notifier.go` | Рассылка напоминаний в своей горутине: `Notifier`, цикл `Start(ctx)` (таймер + `select` на `ctx.Done()`), `nextTrigger` ({11:30, 17:30, 23:30}, таймзона `lib/timeloc`), `notify` (получатели — без записи за часть суток). Интерфейсы `Storage` / `Sender` — на стороне потребителя |
| `storage/storage.go` | Интерфейс `Storage` (`Save` / `Get` / `Update` / `Show` / `GetAll` / `ClaimLegacy` / `RegisterUser` / `UsersWithoutPressure`), модели `Pressure` (ключ — `UserID int64`, `UserName` — отображаемый/аудитный) и `User` |
| `storage/sqlite/sqlite.go` | Реализация на SQLite. `New` настраивает пул (`SetMaxOpenConns(1)`) и DSN (`_busy_timeout`, WAL); есть `Close()`. `Save` — `INSERT ... ON CONFLICT DO NOTHING`, признак вставки из `RowsAffected`. `Get` — запись по ключу `(user_id, date, day_part)`, `sql.ErrNoRows` → `(nil, nil)`. `Update` — перезапись значений по тому же ключу, `RowsAffected() == 0` → `sql.ErrNoRows`. `Show()`/`GetAll()` возвращают `[]storage.Pressure` (форматирование — в `events/telegram`). `RegisterUser` — upsert в таблицу `users`. `UsersWithoutPressure` — подзапрос `NOT EXISTS` по `blood_pressure`. `Init` вызывает механизм миграций |
| `storage/sqlite/migrations.go` | Механизм миграций: срез `migrations []func(ctx, *sql.Tx) error`, версия схемы — `PRAGMA user_version`; каждая миграция в своей транзакции. `migration3` создаёт таблицу `users` |
| `lib/e/e.go` | `Wrap` (nil-safe) / `WrapIfErr` — обёртки над `fmt.Errorf("%w")` |
| `lib/timeloc/timeloc.go` | Единая таймзона учёта (`Asia/Yekaterinburg`) и форматы даты (`DateFormat`, `CSVDateFormat`, `UserDateFormat`); `time.LoadLocation` — один раз при инициализации пакета |
| `events/telegram/commands_test.go`, `clients/telegram/telegram_test.go`, `storage/sqlite/sqlite_test.go`, `notifier/notifier_test.go` | Единственные тестовые файлы |
| `Makefile` | Таргеты `dc_*` для docker compose и `test` |
| `Dockerfile`, `docker-compose.yml` | Сборка и запуск контейнера |
| `.env.dist` | Шаблон окружения (`TG_KEY`, `COMPOSE_PROJECT_NAME`); реальный `.env` в `.gitignore` |
| `data/sqlite/` | Каталог БД (`storage.db` + `-wal` / `-shm`), монтируется как volume; в git не попадает (`*.db*`) |

## Команды сборки и разработки

```sh
go build ./...                                 # сборка
go vet ./...                                   # должен быть чистым
gofmt -l .                                     # должен выводить пустой список
make test                                      # go test ./... -count=1 -race -cover
go test ./events/telegram -run TestDayPart -v  # один тест
go test ./notifier -run TestNextTrigger -v     # один тест пакета notifier
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

**Поток данных:** `event_consumer.Consumer` → `Fetcher.Fetch` (`*Processor` → `Client.Updates`) → `Processor.Process` → `doCmd` → `storage.Storage`. Параллельно в отдельной горутине работает `notifier` с тем же `storage.Storage` и Telegram-клиентом.

- **Интерфейсы объявляются на стороне потребителя.** `events/telegram.Client` существует ради моков; `*clients/telegram.Client` удовлетворяет ему неявно. **Не заменять поле `tg` на конкретный тип** — это сломает тесты. То же для `notifier.Storage` / `notifier.Sender`.
- `*Processor` реализует и `events.Fetcher`, и `events.Processor`, поэтому передаётся дважды: `event_consumer.New(eventsProcessor, eventsProcessor, batchSize)`.
- **Диалог `/add` и подтверждение перезаписи** — state-machine в памяти: `Processor.sessions map[int64]*session` (как `claimed`, без мьютекса: консьюмер однопоточный). Активное состояние перехватывает все не-командные сообщения; любая команда сбрасывает диалог и выполняется как обычно, `/cancel` — отмена с сообщением. При дубликате (быстрый ввод или `/add`) `confirmOverwrite` переиспользует активную сессию или создаёт новую, кладёт в неё отложенные показания (`pendingSys/pendingDia/pendingHr`) и переводит в `stateConfirmOverwrite`; `handleOverwrite` разводит «Перезаписать» → `doOverwrite` (повторная валидация + `storage.Update`) и «Не перезаписывать» → отмена с `msgKeepExisting`. Клавиатуры и их кнопки — только в `messages.go`/`dialog.go`, тексты — только в `messages.go`.
- `events/telegram.DayPart` и `MsgReminder` — экспортированы, потому что их использует пакет `notifier` (метка части суток и шаблон текста напоминания). `notifier.nextTrigger` строит время через `time.Date(..., timeloc.Location())`; `notify` опрашивает `timeloc.Today()`.
- **Ошибки:** в `events/`, `clients/` и `notifier/` — `lib/e` через `defer` в начале функции (`defer func() { err = e.WrapIfErr("...", err) }()`), для чего нужны именованные возвращаемые значения. В `storage/sqlite` — напрямую `fmt.Errorf("...: %w", err)`. Sentinel-ошибки объявляются в пакете-владельце и сравниваются через `errors.Is`.
- **Именование:** пакеты `events/telegram` и `clients/telegram` оба называются `telegram`, поэтому в `main.go` и тестах нужен алиас `tgClient`. Пакет в `consumer/event-consumer` называется `event_consumer` — с подчёркиванием.
- **Тексты для пользователя — только в `events/telegram/messages.go`**, константы с префиксом `msg`. Тесты сравнивают отправленное именно с этими константами, инлайнить строки нельзя. `MsgReminder` — единственная экспортированная константа (ради пакета `notifier`), текст остаётся в `messages.go`.
- **Команды бота** — константы `ShowCmd` / `HelpCmd` / `StartCmd` / `DownloadCmd` в `commands.go`.
- `storage/sqlite.Show()`/`GetAll()` возвращают `[]storage.Pressure`; форматирование строки для пользователя — в `events/telegram`: `formatPressures` + шаблон `msgPressureLine` для `/show` (дата выводится в формате ДД.ММ.ГГГГ через `formatCSVDate`; показания сортируются по части суток: Утро → День → Вечер), `formatCSV` + шапка `msgCSVHeader` + `csvFilename` для `/download`. При изменении формата чинить `messages.go`, `csv.go` (если про CSV), `commands_test.go` (`TestFormatPressures` / `TestFormatCSV` / `TestCSVFilename`) и, если правится выборка, `sqlite_test.go`.
- SQL — только параметризованные запросы (`?`), конкатенация значений в запрос запрещена. **Единственное исключение:** `PRAGMA user_version = N` в `migrations.go` — PRAGMA не поддерживает плейсхолдеры, `N` подставляется через `fmt.Sprintf` из `int`-константы кода (не пользовательский ввод), инъекция невозможна.
- **`context.Context` пробрасывается сверху вниз** до HTTP и SQL: `Consumer.Start(ctx)` → `Fetch(ctx, …)` / `Process(ctx, …)` → `doCmd(ctx, …)` → `Client.Updates/SendMessage/SendDocument(ctx, …)` и `*Context`-методы `*sql.DB`. `context.Background()` / `context.TODO()` в прод-коде вне `main` запрещены; в `storage/sqlite` — только `QueryContext` / `ExecContext` / `QueryRowContext`.
- **Ответ Telegram API проверяется дважды**: HTTP-код (общий хелпер `do`) и поле `ok` (`checkOK` для `SendDocument`, ручной парсинг в `Updates`). Возврат `(nil, nil)` при ошибке API недопустим — ошибка становится `*telegram.APIError` (`error_code` / `description` / `retry_after`).
- **Токен не попадает в текст ошибок.** `Client` хранит его в поле `token` и санитизирует любую ошибку (`sanitize` заменяет токен на `***`, сохраняя цепочку через `Unwrap`) перед возвратом. Наружу пробрасываются только санитизированные ошибки клиента.
- **Регулярные выражения — пакетные `var … = regexp.MustCompile(…)`** (`pressureRe`, `numberRe`), компиляция внутри функции запрещена.
- `msgError` в `messages.go` — при сбое БД пользователь получает ответ, а не тишину.
- **Таймзона и формат даты — только через `lib/timeloc`** (`timeloc.Now()`, `timeloc.DateFormat`), не хардкодить строки в бизнес-коде.
- Язык кода, комментариев, сообщений и коммитов — **русский**.
- Форматирование — `gofmt`, табы, без исключений.
- `TG_KEY` читается только из окружения (`os.Getenv` в `main.go`, `mustToken()` делает `log.Fatal` при пустом токене). Хардкодить токен, логировать его или подставлять в сообщения об ошибках запрещено — он входит в `basePath` HTTP-клиента. Клиент санитизирует ошибки (`sanitize`), поэтому наружу **логировать можно только санитизированные ошибки клиента**; `err.Error()` от `*url.Error` напрямую не пробрасывать.
- `.env` и `*.db` в `.gitignore`; шаблон — `.env.dist`, в нём только плейсхолдеры. `docker compose` подхватывает `.env` автоматически.
- `.dockerignore` исключает `.env`, `data/`, `.git` и `*.md` — секреты и документация в образ не попадают.

## Правила тестирования

Запуск: `make test` (включает `-race -cover -count=1`), точечно — `go test ./events/telegram -run TestDayPart -v`.

Ключевые правила:

- SQLite-тесты используют файл в `t.TempDir()`, а **не** `:memory:` — пул `*sql.DB` открывает несколько соединений, каждое со своей пустой in-memory БД (флаки).
- Хелпер `today(t)` в `sqlite_test.go` берёт дату в `Asia/Yekaterinburg`, как и `Show`. Смена таймзоны в проде ломает тест.
- В `notifier/notifier_test.go` времена строятся через `time.Date(..., timeloc.Location())`, как в `nextTrigger`; смена таймзоны в проде ломает тест (аналог правила выше).
- Новые тесты пишутся в существующие файлы, в том же table-driven стиле.
