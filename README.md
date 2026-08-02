# Blood Pressure Bot

## Краткое описание проекта

Telegram-бот для записи и просмотра показаний артериального давления. Пользователь может ввести показания быстрым способом — сообщение вида `120 80 70` — или пошагово через `/add`: выбор даты (по умолчанию — сегодня), выбор части суток (Утро/День/Вечер) и ввод всех показаний одним сообщением вида `120 80 70` с валидацией диапазонов; `/show` выводит показания за сегодня, `/download` отправляет CSV-файл со всеми показаниями за всё время. При дубликате (показания уже есть за эту дату и часть суток) бот показывает старые и новые показания и спрашивает, перезаписать ли запись. «Сегодня», часть суток и время срабатывания напоминаний считаются в **персональной таймзоне** пользователя: `getChat` → `utc_offset` (кэшируется и пишется в БД), при неизвестном offset (0 или ошибка) — fallback на серверную таймзону `lib/timeloc`. Отдельный пакет `notifier` рассылает напоминания «Пора передать показания за {часть суток}» в 11:30 / 17:30 / 23:30 локального времени каждому пользователю, ещё не передавшему показания за текущую часть суток своего дня.

## Структура репозитория

| Путь | Назначение |
|---|---|
| `main.go` | Сборка зависимостей и старт. Путь к БД, хост API и `batchSize` — константы, **не env**. Первый аргумент `export\|delete\|help` переключает в CLI-режим (`runCLI`), бот запускается без аргументов как раньше и не требует `TG_KEY` для CLI. Рядом с консьюмером в отдельной горутине запускается `notifier` |
| `clients/telegram/telegram.go` | HTTP-клиент Bot API: `Updates`, `SendMessage`, `SendKeyboard`, `RemoveKeyboard`, `SendDocument`, `GetChat`, `doRequest`. Методы API — константы `getUpdatesMethod` / `sendMessageMethod` / `sendDocumentMethod` / `getChatMethod`. Общие хелперы `do` (выполнение запроса, проверка HTTP-статуса, `sanitize`) и `checkOK` (поле `ok` → `APIError`); multipart-тело `SendDocument` собирает `multipartBody` |
| `clients/telegram/types.go` | DTO ответов API (`Update`, `IncomingMessage`, `From`, `Chat`, `ChatFullInfo` с `utc_offset`, обёртки `UpdatesResponse` / `GetChatResponse`) + поля ошибок API (`ok`, `error_code`, `description`, `parameters.retry_after`), типизированная `APIError` и её билдеры `toError`; клавиатуры `ReplyKeyboardMarkup` / `ReplyKeyboardRemove` |
| `events/type.go` | Интерфейсы `Fetcher` / `Processor` (оба принимают `ctx context.Context`), тип `Event` и `Type` |
| `events/telegram/telegram.go` | `*Processor` — реализует **оба** интерфейса; интерфейс `Client` объявлен на стороне потребителя ради моков (включая `GetChat`); поле `utcOffsets` — кэш таймзон пользователей |
| `events/telegram/userloc.go` | Персональная таймзона: `ensureTimezone` (ленивый `getChat` → кэш + персист `SetUTCOffset`, при ошибке повтор на следующем сообщении), `userLoc` / `userNow` / `userToday` (fallback — `timeloc`) |
| `events/telegram/commands.go` | Роутинг команд, `savePressure`, `save`, `confirmOverwrite`, `show`, `download`, `DayPart`, `isPressure`, `getPressures`. Вызов `storage.RegisterUser` в `doCmd` при каждом сообщении, затем `ensureTimezone` |
| `events/telegram/dialog.go` | State-machine пошагового ввода `/add` и подтверждения перезаписи: `session` по `user_id`, шаги дата → часть суток → показания одним сообщением (`120 80 70`) → при дубликате `stateConfirmOverwrite` (`handleOverwrite` → `doOverwrite`); reply-клавиатуры (`dateKeyboard`, `dayPartKeyboard`, `overwriteKeyboard`), `parseUserDate`, `dayPartKey`. «Сегодня» и подсказка части суток — в локали пользователя |
| `events/telegram/csv.go` | Генерация CSV-выгрузки `/download`: `formatCSV` (BOM + CRLF, разделитель `;`, фиксированный порядок утро→день→вечер, значения показаний как `систолическое/диастолическое/пульс`, пустые ячейки) и `csvFilename` |
| `events/telegram/messages.go` | **Все** тексты для пользователя, константы с префиксом `msg` (включая шапку CSV `msgCSVHeader`, промпты и кнопки диалога `/add` и подтверждения перезаписи: `msgDuplicatePrompt`, `msgOverwriteButton`, `msgKeepButton`, `msgOverwritten`, `msgKeepExisting`). `MsgReminder` — единственная экспортированная, нужна пакету `notifier` |
| `consumer/consumer.go` | Интерфейс `Consumer` с сигнатурой `Start(ctx context.Context) error` |
| `consumer/event-consumer/` | Пакет `event_consumer`: цикл fetch → process, завершается по отмене `ctx`; `recover` вокруг обработки каждого события |
| `notifier/notifier.go` | Рассылка напоминаний в своей горутине: `Notifier`, цикл `Start(ctx)` (таймер + `select` на `ctx.Done()`), `nextTrigger(now, loc)` ({11:30, 17:30, 23:30}), `isReminderMinute`, персональные таймзоны пользователей (`userLoc` по `utc_offset`, fallback `lib/timeloc`), `notify` (получатели — без записи за часть суток их дня). Интерфейсы `Storage` (`AllUsers` + `Get`) / `Sender` — на стороне потребителя |
| `storage/storage.go` | Интерфейс `Storage` (`Save` / `Get` / `Update` / `Show(ctx, userID, date)` / `GetAll` / `ClaimLegacy` / `RegisterUser` / `SetUTCOffset` / `AllUsers`), модели `Pressure` (ключ — `UserID int64`, `UserName` — отображаемый/аудитный) и `User` (с `UTCOffset`), тип `Filter` — критерии выборки/удаления для CLI (`UserID`/`UserName`/`From`/`To`, все поля опциональны, даты — `DateFormat`) |
| `storage/sqlite/sqlite.go` | Реализация на SQLite. `New` настраивает пул (`SetMaxOpenConns(1)`) и DSN (`_busy_timeout`, WAL); есть `Close()`. `Save` — `INSERT ... ON CONFLICT DO NOTHING`, признак вставки из `RowsAffected`. `Get` — запись по ключу `(user_id, date, day_part)`, `sql.ErrNoRows` → `(nil, nil)`. `Update` — перезапись значений по тому же ключу, `RowsAffected() == 0` → `sql.ErrNoRows`. `Show(ctx, userID, date)`/`GetAll()` возвращают `[]storage.Pressure` (форматирование — в `events/telegram`). `ExportAll(ctx, filter)`/`Delete(ctx, filter)` — **только на `*Storage`** (не в интерфейсе, не трогает моки и слой бота): динамический `WHERE` через `buildFilterWhere` (значения только через `?`), `ORDER BY user_id, date`, legacy-строки (NULL `user_id`) попадают в выборку; `Delete` возвращает `RowsAffected`. `RegisterUser` — upsert в таблицу `users`. `SetUTCOffset` — сохранение персональной таймзоны. `AllUsers` — все пользователи с `utc_offset` (отбор получателей — в `notifier`). `Init` вызывает механизм миграций |
| `storage/sqlite/migrations.go` | Механизм миграций: срез `migrations []func(ctx, *sql.Tx) error`, версия схемы — `PRAGMA user_version`; каждая миграция в своей транзакции. `migration3` создаёт таблицу `users`, `migration4` добавляет колонку `utc_offset` |
| `cli/` | Командная строка для управления БД (`go run . export\|delete\|help`): `cli.go` — `Run(args, store)`, интерфейс `store` (`Init`/`ExportAll`/`Delete`/`Close`) на стороне потребителя, роутинг, общие флаги `-user-id`/`-user-name`/`-from`/`-to` (`parseFilter`/`parseCLIDate` ДД.ММ.ГГГГ→ГГГГ-ММ-ДД, проверка `from <= to`); `export.go` — `formatExportCSV` (BOM+CRLF, шапка `User_ID;User_name;Дата;Утро;День;Вечер`, одна строка на (пользователь, дата), пустые ячейки, `User_ID` для legacy — пустой), файл по умолчанию `export_<сегодня>.csv`, при пустом результате файл не создаётся; `delete.go` — без флага `--yes` отказ, без фильтров — удаление всех записей, вывод «Удалено N записей»; `help.go` — справка. Порядок частей суток — локальная константа `["утро","день","вечер"]`, зависимости на слой бота нет |
| `lib/e/e.go` | `Wrap` (nil-safe) / `WrapIfErr` — обёртки над `fmt.Errorf("%w")` |
| `lib/timeloc/timeloc.go` | Единая таймзона учёта (`Asia/Yekaterinburg`) и форматы даты (`DateFormat`, `CSVDateFormat`, `UserDateFormat`); `time.LoadLocation` — один раз при инициализации пакета |
| `events/telegram/commands_test.go`, `clients/telegram/telegram_test.go`, `storage/sqlite/sqlite_test.go`, `notifier/notifier_test.go`, `cli/cli_test.go` | Единственные тестовые файлы |
| `Makefile` | Таргеты `dc_*` для docker compose, `test`, `lint` и `cli_*` (`cli_help`, `cli_export`, `cli_delete`) |
| `Dockerfile`, `docker-compose.yml` | Сборка и запуск контейнера |
| `.github/workflows/ci.yml`, `.golangci.yml` | CI в GitHub Actions (тесты и линтер при `push` / `pull_request`) и конфиг golangci-lint |
| `.env.dist` | Шаблон окружения (`TG_KEY`, `COMPOSE_PROJECT_NAME`); реальный `.env` в `.gitignore` |
| `data/sqlite/` | Каталог БД (`storage.db` + `-wal` / `-shm`), монтируется как volume; в git не попадает (`*.db*`) |

## Команды сборки и разработки

```sh
go build ./...                                 # сборка
go vet ./...                                   # должен быть чистым
gofmt -l .                                     # должен выводить пустой список
make test                                      # go test ./... -count=1 -race -cover
make lint                                      # golangci-lint run ./... (golangci-lint v2)
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

CLI для управления БД (запускается без `TG_KEY`, ходит в ту же БД, что и бот):

```sh
go run . help                                                # справка
make cli_help
go run . export                                              # все записи в export_<сегодня>.csv
make cli_export ARGS="-user-name alice -from 01.01.2026"     # фильтр по user_name + диапазон дат
make cli_export ARGS="-user-id 7 -out /tmp/export.csv"
go run . delete -yes                                         # ВНИМАНИЕ: удаляет ВСЕ записи
make cli_delete ARGS="-yes"
make cli_delete ARGS="-user-name bob -from 01.01.2026 -to 31.01.2026 -yes"
```

Формат CSV: UTF-8 с BOM, разделитель `;`, одна строка на (пользователь, дата), отсутствующие части суток — пустые ячейки. Шапка: `User_ID;User_name;Дата;Утро;День;Вечер`. Для `delete` без `--yes` — отказ с предупреждением; без фильтров команда удаляет все записи. При активном боте (WAL, `_busy_timeout=5000`) возможна задержка записи до 5с; для `delete` безопаснее останавливать бот.

- Зависимости ставятся штатным `go mod download`; vendor-каталога нет.
- **`CGO_ENABLED=0` ломает и сборку, и тесты** — из-за `go-sqlite3`. В Dockerfile статическая линковка сделана через `-tags netgo -ldflags '-extldflags "-static"'`, а не отключением cgo.
- Линтер `golangci-lint` (`make lint`, конфиг — `.golangci.yml`) и CI в GitHub Actions (`.github/workflows/ci.yml`) добавлены; CI прогоняет `go build` / `go vet` / `gofmt` / `make test` и `golangci-lint` при каждом `push` и `pull_request`. Локальная установка линтера: `go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.12` (в CI версия берётся из `ci.yml`).
- Проект на `go 1.26` (`go.mod`), в Dockerfile — `golang:1.26`; `go-sqlite3` — `v1.14.49`.

## Стиль кода и конвенции

**Поток данных:** `event_consumer.Consumer` → `Fetcher.Fetch` (`*Processor` → `Client.Updates`) → `Processor.Process` → `doCmd` → `storage.Storage`. Параллельно в отдельной горутине работает `notifier` с тем же `storage.Storage` и Telegram-клиентом.

- **Интерфейсы объявляются на стороне потребителя.** `events/telegram.Client` существует ради моков; `*clients/telegram.Client` удовлетворяет ему неявно. **Не заменять поле `tg` на конкретный тип** — это сломает тесты. То же для `notifier.Storage` / `notifier.Sender`.
- `*Processor` реализует и `events.Fetcher`, и `events.Processor`, поэтому передаётся дважды: `event_consumer.New(eventsProcessor, eventsProcessor, batchSize)`.
- **Диалог `/add` и подтверждение перезаписи** — state-machine в памяти: `Processor.sessions map[int64]*session` (как `claimed`, без мьютекса: консьюмер однопоточный). Активное состояние перехватывает все не-командные сообщения; любая команда сбрасывает диалог и выполняется как обычно, `/cancel` — отмена с сообщением. При дубликате (быстрый ввод или `/add`) `confirmOverwrite` переиспользует активную сессию или создаёт новую, кладёт в неё отложенные показания (`pendingSys/pendingDia/pendingHr`) и переводит в `stateConfirmOverwrite`; `handleOverwrite` разводит «Перезаписать» → `doOverwrite` (повторная валидация + `storage.Update`) и «Не перезаписывать» → отмена с `msgKeepExisting`. Клавиатуры и их кнопки — только в `messages.go`/`dialog.go`, тексты — только в `messages.go`.
- `events/telegram.DayPart` и `MsgReminder` — экспортированы, потому что их использует пакет `notifier` (метка части суток и шаблон текста напоминания). `notifier.nextTrigger(now, loc)` строит время через `time.Date(..., loc)` (персональная локация пользователя или `timeloc.Location()`); `notify` опрашивает `AllUsers` + `Get` с датой и частью суток в локали каждого пользователя.
- **Ошибки:** в `events/`, `clients/` и `notifier/` — `lib/e` через `defer` в начале функции (`defer func() { err = e.WrapIfErr("...", err) }()`), для чего нужны именованные возвращаемые значения. В `storage/sqlite` — напрямую `fmt.Errorf("...: %w", err)`. Sentinel-ошибки объявляются в пакете-владельце и сравниваются через `errors.Is`.
- **Именование:** пакеты `events/telegram` и `clients/telegram` оба называются `telegram`, поэтому в `main.go` и тестах нужен алиас `tgClient`. Пакет в `consumer/event-consumer` называется `event_consumer` — с подчёркиванием.
- **Тексты для пользователя — только в `events/telegram/messages.go`**, константы с префиксом `msg`. Тесты сравнивают отправленное именно с этими константами, инлайнить строки нельзя. `MsgReminder` — единственная экспортированная константа (ради пакета `notifier`), текст остаётся в `messages.go`.
- **Команды бота** — константы `ShowCmd` / `HelpCmd` / `StartCmd` / `DownloadCmd` в `commands.go`.
- `storage/sqlite.Show(ctx, userID, date)`/`GetAll()` возвращают `[]storage.Pressure`; форматирование строки для пользователя — в `events/telegram`: `formatPressures` + шаблон `msgPressureLine` для `/show` (дата выводится в формате ДД.ММ.ГГГГ через `formatCSVDate`; показания сортируются по части суток: Утро → День → Вечер), `formatCSV` + шапка `msgCSVHeader` + `csvFilename` для `/download`. При изменении формата чинить `messages.go`, `csv.go` (если про CSV), `commands_test.go` (`TestFormatPressures` / `TestFormatCSV` / `TestCSVFilename`) и, если правится выборка, `sqlite_test.go`.
- SQL — только параметризованные запросы (`?`), конкатенация значений в запрос запрещена. **Единственное исключение:** `PRAGMA user_version = N` в `migrations.go` — PRAGMA не поддерживает плейсхолдеры, `N` подставляется через `fmt.Sprintf` из `int`-константы кода (не пользовательский ввод), инъекция невозможна.
- **`context.Context` пробрасывается сверху вниз** до HTTP и SQL: `Consumer.Start(ctx)` → `Fetch(ctx, …)` / `Process(ctx, …)` → `doCmd(ctx, …)` → `Client.Updates/SendMessage/SendDocument(ctx, …)` и `*Context`-методы `*sql.DB`. `context.Background()` / `context.TODO()` в прод-коде вне `main` запрещены; в `storage/sqlite` — только `QueryContext` / `ExecContext` / `QueryRowContext`. **Исключение — пакет `cli`:** короткоживущий CLI-режим запускается из `main` без цепочки `Consumer.Start(ctx)`, поэтому методы хранилища вызываются с `context.Background()` (сигнатура `Run(args, store)` фиксирована планом, контекст не пробрасывается).
- **Ответ Telegram API проверяется дважды**: HTTP-код (общий хелпер `do`) и поле `ok` (`checkOK` для `SendDocument`, ручной парсинг в `Updates`). Возврат `(nil, nil)` при ошибке API недопустим — ошибка становится `*telegram.APIError` (`error_code` / `description` / `retry_after`).
- **Токен не попадает в текст ошибок.** `Client` хранит его в поле `token` и санитизирует любую ошибку (`sanitize` заменяет токен на `***`, сохраняя цепочку через `Unwrap`) перед возвратом. Наружу пробрасываются только санитизированные ошибки клиента.
- **Регулярные выражения — пакетные `var … = regexp.MustCompile(…)`** (`pressureRe`, `numberRe`), компиляция внутри функции запрещена.
- `msgError` в `messages.go` — при сбое БД пользователь получает ответ, а не тишину.
- **Таймзона пользователя — персональная, через `getChat.utc_offset`** (кэш `utcOffsets` + колонка `users.utc_offset`), fallback на `lib/timeloc`. Все вычисления «сегодня / часть суток» при записи, в `/show`, в диалоге `/add` и в `notifier` идут в локали пользователя (`userNow`/`userToday` в `events/telegram`, `userLoc`/`isReminderMinute` в `notifier`). Форматы даты — только через `timeloc.DateFormat` и производные, не хардкодить строки в бизнес-коде. **Ограничение API:** `utc_offset == 0` неразличим («реальный UTC» vs «неизвестно») → трактуется как неизвестный, fallback на серверную таймзону.
- Язык кода, комментариев, сообщений и коммитов — **русский**.
- Форматирование — `gofmt`, табы, без исключений.
- `TG_KEY` читается только из окружения (`os.Getenv` в `main.go`, `mustToken()` делает `log.Fatal` при пустом токене). Хардкодить токен, логировать его или подставлять в сообщения об ошибках запрещено — он входит в `basePath` HTTP-клиента. Клиент санитизирует ошибки (`sanitize`), поэтому наружу **логировать можно только санитизированные ошибки клиента**; `err.Error()` от `*url.Error` напрямую не пробрасывать.
- `.env` и `*.db` в `.gitignore`; шаблон — `.env.dist`, в нём только плейсхолдеры. `docker compose` подхватывает `.env` автоматически.
- `.dockerignore` исключает `.env`, `data/`, `.git` и `*.md` — секреты и документация в образ не попадают.

## Правила тестирования

Запуск: `make test` (включает `-race -cover -count=1`), точечно — `go test ./events/telegram -run TestDayPart -v`.

Ключевые правила:

- SQLite-тесты используют файл в `t.TempDir()`, а **не** `:memory:` — пул `*sql.DB` открывает несколько соединений, каждое со своей пустой in-memory БД (флаки).
- Хелпер `today(t)` в `sqlite_test.go` берёт дату в `Asia/Yekaterinburg` для вызовов `Show(ctx, id, date)`. Смена таймзоны в проде ломает тест.
- В `notifier/notifier_test.go` времена строятся через `time.Date(..., timeloc.Location())`, как в `nextTrigger(now, loc)`; персональные таймзоны тестируются через `time.FixedZone` с фиксированными UTC-моментами. Смена таймзоны в проде ломает тест (аналог правила выше).
- Новые тесты пишутся в существующие файлы, в том же table-driven стиле.
