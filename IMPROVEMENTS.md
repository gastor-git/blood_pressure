# План: Этап 2 и Этап 3

Этап 1 выполнен. Ниже — только реализация Этапов 2 и 3.

Нумерация пунктов (14, 15, 16, 18, 2, 11) соответствует исходному аудиту.

Общая верификация после каждого этапа: `go build ./...` → `go vet ./...` → `gofmt -l .` → `make test`.

Приняты решения:

- **dayPart** — код приводится к тексту `msgAlreadyExists` (утро 00:00–11:59, день 12:00–17:59, вечер 18:00–23:59).
- **Валидация** — мягкие медицинские границы + обязательно `systolic > diastolic`.
- **Миграция данных** — ленивый backfill по `username`.
- **TOCTOU** — UNIQUE-индекс, `Save` возвращает признак вставки, `IsExists` удаляется.
- Рефакторинг `Show` (аудит № 18) **объединён** с переходом на `user_id` (№ 2): сигнатура `Show` меняется один раз, сразу на `Show(ctx, userID) ([]Pressure, error)`. Поэтому № 18 живёт в Этапе 3, а Этап 2 — только логика.

---

## Этап 2 — логика (правит тесты, схему БД не трогает)

Пункты аудита: 14, 15, 16.

### 2.1 `dayPart` — границы по тексту `msgAlreadyExists`

`events/telegram/commands.go:119-129`. Новая логика: `hour < 12` → утро, `12 <= hour < 18` → день, иначе вечер. Одной правкой чинит и полночь (баг 14), и 12:xx (баг 15); `messages.go:15` менять не нужно.

Литералы `"утро"/"день"/"вечер"` → константы `dayPartMorning/dayPartDay/dayPartEvening` в `commands.go` (остаток п. 20 аудита). Новый тип не вводим — `storage.Pressure.DayPart` остаётся `string`.

Тесты: `commands_test.go:110-136` — переписать кейсы: `00:00`/`00:59` → «утро», `12:00`/`12:59` → «день», `18:00`/`18:59` → «вечер»; добавить `11:59`, `17:59`, `23:59`.

### 2.2 Валидация диапазонов

Разделяем **роутинг** и **валидацию**:

- `isPressure` (regexp) остаётся гейтом роутинга — `TestIsPressure` **не ломается**, кейс `{"999 999 999", true}` остаётся `true` (правится только комментарий на `commands_test.go:84`: regexp пропускает по формату, диапазоны проверяет `validatePressure`).
- Новая `validatePressure(sys, dia, hr int) error` в `commands.go`: систолическое 60–260, диастолическое 30–200, пульс 30–220, обязательно `sys > dia`. Sentinel-ошибки объявляются рядом.
- `savePressure` парсит три значения через `strconv.Atoi`, при ошибке валидации шлёт новое `msgInvalidPressure` (`messages.go`) и возвращает `nil` — это не сбой сервиса, а пользовательский ввод.

Тип колонок в БД остаётся `TEXT` — конвертация в `INTEGER` вне скоупа.

Тесты: новый `TestValidatePressure` (table-driven, границы включительно/исключительно, `sys <= dia`), `TestSavePressure_Invalid` (`Save` не вызван, отправлен `msgInvalidPressure`).

### Верификация Этапа 2

`go build ./...` → `go vet ./...` → `gofmt -l .` → `make test`.

---

## Этап 3 — ключ `user_id`, миграции, рефакторинг `Show` (меняет схему)

Пункты аудита: 2, 11, 18. Боевая БД: 3 строки, один пользователь `tsybaevArt`, дубликатов по `(date, day_part, user_name)` нет; индексов и механизма миграций нет.

### 3.1 Механизм миграций (делается первым)

`storage/sqlite/migrations.go`: срез `migrations []func(context.Context, *sql.Tx) error`, версия схемы — `PRAGMA user_version`. `Init` читает версию, применяет недостающие миграции (каждую — в своей транзакции) и поднимает `user_version`.

Осознанное отступление от правила «только параметризованный SQL» (`AGENTS.md`): `PRAGMA user_version = N` не поддерживает плейсхолдеры; `N` — `int`-константа из кода, не пользовательский ввод. Зафиксировать комментарием в коде и строкой в `AGENTS.md`.

- Миграция 1 — текущий `CREATE TABLE IF NOT EXISTS blood_pressure (...)` из `sqlite.go:112`. Боевая БД с `user_version = 0` уже ей соответствует, повторное применение безопасно.
- Миграция 2 — см. 3.2.

Тесты: `TestMigrations_Idempotent` (двойной `Init`); `TestMigrations_FromLegacySchema` (создать таблицу по старой схеме с `user_version = 0`, прогнать `Init`, проверить наличие колонки `user_id` и индексов).

### 3.2 Схема (миграция 2)

1. `ALTER TABLE blood_pressure ADD COLUMN user_id INTEGER` (NULL для старых строк).
2. Дедупликация-страховка: `DELETE FROM blood_pressure WHERE rowid NOT IN (SELECT MIN(rowid) FROM blood_pressure GROUP BY date, day_part, user_name)`.
3. `CREATE UNIQUE INDEX IF NOT EXISTS idx_pressure_key ON blood_pressure(date, day_part, user_id)` — `NULL` в SQLite различны, поэтому legacy-строки индексу не мешают.
4. `CREATE INDEX IF NOT EXISTS idx_pressure_legacy ON blood_pressure(user_name) WHERE user_id IS NULL` — частичный индекс под ленивый backfill (после переноса практически пуст).

### 3.3 Проброс `user_id` через слои

- `clients/telegram/types.go:59-61` — `From{ID int64, Username string}`.
- `events/telegram/telegram.go:24-27,101-106` — `Meta.UserID int64`.
- `doCmd(ctx, text string, chatID int, userID int64, username string)`.
- `storage.Pressure` — поле `UserID int64`; `UserName` остаётся отображаемым/аудитным, в ключ не входит.
- Логирование в `commands.go:33` — по `userID`, без username (меньше персональных данных в логах).

### 3.4 Рефакторинг `Show` (объединено с № 18)

Сигнатура меняется один раз, сразу под `user_id`:

- `storage/storage.go` — `Show(ctx context.Context, userID int64) ([]Pressure, error)`.
- `storage/sqlite/sqlite.go:56-96` — фильтр по `user_id`, убрать `strings.Builder` и русский текст, вернуть срез.
- `events/telegram/commands.go:95-109` — `show` получает срез; при `len(res) == 0` → `msgNoSavedPressure`, иначе `formatPressures(res)`.
- `formatPressures` — в `commands.go`; шаблон строки — константа `msgPressureLine` в `messages.go`, формат сохраняем байт-в-байт (`"Дата: %s, часть суток: %s, показания: %s/%s/%s\n\n"`), сборка через `strings.Builder`.

### 3.5 Дедупликация без TOCTOU (№ 11)

`storage.Storage` становится:

```go
Save(ctx context.Context, p *Pressure) (bool, error)   // false — запись за эту часть суток уже есть
Show(ctx context.Context, userID int64) ([]Pressure, error)
ClaimLegacy(ctx context.Context, userID int64, userName string) error
```

`IsExists` удаляется из интерфейса и из `sqlite.go:99-109`; `Remove` уже удалён в Этапе 1. `Save` — один запрос `INSERT ... ON CONFLICT(date, day_part, user_id) DO NOTHING`, признак вставки — из `RowsAffected()`. `savePressure` шлёт `msgAlreadyExists` при `false` — гонка исчезает физически.

### 3.6 Ленивый backfill старых записей

`ClaimLegacy`: `UPDATE OR IGNORE blood_pressure SET user_id = ? WHERE user_id IS NULL AND user_name = ?`.

Вызов — из `doCmd` до диспетчеризации, только при непустом `username`, один раз на пользователя за время жизни процесса (map в `Processor`; консьюмер однопоточный, мьютекс не нужен — `-race` в `make test` подтвердит). `OR IGNORE` защищает от конфликта с UNIQUE-индексом; неперенесённые остатки остаются невидимыми, но запись не ломают.

### 3.7 Тесты Этапа 3

`sqlite_test.go`:

- `TestSave_Duplicate` — второй `Save` → `false`, в таблице одна строка.
- `TestShow_ByUserID` — разные `user_id` не видят записи друг друга (в т. ч. с одинаковым пустым `user_name`); `Show` возвращает `[]Pressure`.
- `TestClaimLegacy` — строка без `user_id` становится видна в `Show(userID)`.
- `TestClaimLegacy_OtherUserUntouched`.
- Правка `TestSave_IsExists`, `TestShow_Today`, `TestShow_OtherDay`, `TestShow_Empty` под новые сигнатуры и срез.

`commands_test.go`:

- `mockStorage`: `Save` → `(bool, error)`, `Show` → срез, удалить `IsExists`/`Remove`, добавить `ClaimLegacy` со счётчиком.
- `TestSavePressure_Duplicate` — на `saveFunc → false`.
- `TestShow_WithData` — проверяет результат `formatPressures`.
- `TestFormatPressures` (одна и несколько записей).
- `TestClaimLegacy_CalledOncePerUser`.

### 3.8 Порядок работ внутри Этапа 3

1. Механизм миграций + перенос текущего DDL в миграцию 1 (поведение не меняется, тесты зелёные).
2. Миграция 2 + `From.ID` → `Meta` → `Pressure.UserID`.
3. `Show(userID) ([]Pressure, error)` + `formatPressures` (объединённый № 18).
4. `Save` с `ON CONFLICT DO NOTHING`, удаление `IsExists`.
5. `ClaimLegacy` + ленивый вызов из `doCmd`.
6. Обновление тестов и `AGENTS.md` (новые сигнатуры `Storage`, механизм миграций, исключение для `PRAGMA`).

### Верификация Этапа 3

`go build ./...` → `go vet ./...` → `gofmt -l .` → `make test`, плюс ручная проверка на копии боевой БД: `cp data/sqlite/storage.db /tmp/...`, прогон `Init`, проверка что после первого сообщения от `tsybaevArt` его 3 записи видны в `/show`.

---

## Вне скоупа Этапов 2 и 3

| Пункт | Почему |
|---|---|
| Ретрай: `offset` сдвигается до обработки (аудит № 10) | Требует переработки контракта `Fetcher`/`Processor` — отдельный этап |
| `systolic`/`diastolic`/`heart_rate` → `INTEGER` | Расширяет миграцию, выгоды для текущей логики нет |
| Структурное логирование, уровни (`log/slog`) | Отдельная задача, не связана с багами |
