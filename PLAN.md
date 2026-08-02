# План: CLI для управления БД

## Архитектура

Точка входа — подкоманды в `main.go` (`go run . export|delete|help`). Логика CLI выносится в новый пакет `cli/`, который потребляет интерфейс (объявлен на стороне потребителя, как принято в проекте) с методами выборки/удаления. Бот запускается без аргументов как раньше, `TG_KEY` для CLI не требуется.

## 1. `storage/storage.go` — общий фильтр

Добавить тип `Filter`:

```go
type Filter struct {
    UserID   *int64   // nil — без фильтра
    UserName *string  // точное совпадение по user_name
    From     *string  // включительно, формат DateFormat (YYYY-MM-DD)
    To       *string  // включительно, формат DateFormat
}
```

## 2. `storage/sqlite/sqlite.go` — новые методы конкретного типа

Методы добавляются только на `*Storage` (не в интерфейс `storage.Storage` — не трогаем моки `commands_test.go` и слой бота):

- `ExportAll(ctx, filter storage.Filter) ([]storage.Pressure, error)` — динамический `WHERE` с параметрами `?` (user_id / user_name / date >= / date <=), `ORDER BY user_id, date`. Никакой конкатенации значений в SQL.
- `Delete(ctx, filter storage.Filter) (int64, error)` — `DELETE ... WHERE`, возвращает `RowsAffected`.
- Хелпер построения `WHERE` + среза аргументов (общий для обоих).

Примечание: legacy-строки (`user_id IS NULL`) попадают в экспорт и удаление, фильтр по `user_name` их находит, по `user_id` — нет.

## 3. Новый пакет `cli/`

- `cli/cli.go` — `Run(args []string, s store) error`; интерфейс `store { Init; ExportAll; Delete; Close }`; роутинг `export|delete|help`, на незнакомую команду — ошибка с подсказкой `help`. Каждая подкоманда парсит свой `flag.FlagSet`.
- `cli/export.go` — флаги `-user-id N`, `-user-name NAME`, `-from DD.MM.YYYY`, `-to DD.MM.YYYY`, `-out PATH` (по умолчанию `export_<сегодня>.csv`). `formatExportCSV` — по аналогии с `/download`, но с двумя колонками идентификации пользователя. Заголовок **`User_ID;User_name;Дата;Утро;День;Вечер`**: строка на (пользователь, дата), отсутствующие части суток — пустые ячейки, BOM + CRLF, сортировка по дате. `User_ID` — из записи, `User_name` — `user_name` (при пустом — пустая ячейка; `User_ID` для legacy-строк пустой). Если данных нет — сообщение и без создания файла.
- `cli/delete.go` — те же флаги + `-yes`. Без `--yes` — отказ с предупреждением; без фильтров = удаление всех данных. Печать «Удалено N записей».
- `cli/help.go` — текст с описанием формата и опций всех команд + примеры.
- Хелпер `parseCLIDate` — `ДД.ММ.ГГГГ` → `YYYY-MM-DD` (валидация через `timeloc.UserDateFormat`/`DateFormat`, проверка `from <= to`).
- Порядок частей суток — локальная константа `["утро","день","вечер"]` (в `events/telegram` они не экспортируются; не тащим зависимость на слой бота).

## 4. `main.go` — диспетчеризация

До запуска бота: если `os.Args[1]` ∈ {`export`,`delete`,`help`} → `runCLI()`: открыть `sqlite.New(sqliteStoragePath)` (та же константа), `Init`, `cli.Run(os.Args[1:], s)`, `Close`, выход с кодом 0/1. `mustToken()` вызывается только в ветке бота.

## 5. `Makefile` — алиасы

```make
cli_help:   go run . help
cli_export: go run . export $(ARGS)
cli_delete: go run . delete $(ARGS)
```

Примеры: `make cli_export ARGS="--user-name alice --from 01.01.2026"`, `make cli_delete ARGS="--yes"`.

## 6. Тесты

- `storage/sqlite/sqlite_test.go` — `TestExportAll` / `TestDelete` (без фильтра, по `user_id`, по `user_name`, по диапазону дат, комбинированные, пустой результат, счётчик удалённых). В том же table-driven стиле, файл в `t.TempDir()`.
- `cli/cli_test.go` — `formatExportCSV` (заголовок `User_ID;User_name;Дата;Утро;День;Вечер`, группировка по пользователю/дате, пустые ячейки), `parseCLIDate` (валид/невалид), построение `storage.Filter` из флагов, отказ `delete` без `--yes`. Для `Run` — фейковый `store`.

## 7. `README.md`

Обновить: структуру репозитория (строка про `cli/`), раздел команд разработки (документация CLI и новых Makefile-таргетов), строку Makefile в таблице.

## Верификация

`go build ./...` → `go vet ./...` → `gofmt -l .` → `make test`.

Замечание по эксплуатации: CLI ходит в ту же БД, что и запущенный бот (WAL, `_busy_timeout=5000`) — при активном боте возможна задержка записи до 5с; для «delete» безопаснее останавливать бот.
