# План: команда `/download` для Telegram-бота

Команда формирует CSV-файл с показаниями текущего пользователя за всё время и отправляет его через Telegram API (`sendDocument`).

## Решения

- Разделитель CSV — `;` (точка с запятой), для совместимости с русским Excel.
- Пустые данные — сообщение `msgNoSavedPressure` (файл не отправляем).
- Пустой username — имя файла `user_{дата}.csv`.

Формат заголовка (CSV не умеет вложенные шапки, подколонки «плоским» списком):
```
Дата;Утро систолическое;Утро диастолическое;Утро пульс;День систолическое;День диастолическое;День пульс;Вечер систолическое;Вечер диастолическое;Вечер пульс
```
Одна строка на дату; отсутствующие части суток — пустые ячейки. Значения в БД — строки, кавычек/запятых/точки с запятой в них нет, поэтому собираем `strings.Join(fields, ";")` без `encoding/csv` (стандартный `csv.Writer` не умеет менять разделитель).

## 1. Storage-слой

**`storage/storage.go`** — добавить в интерфейс:
```go
GetAll(ctx context.Context, userID int64) ([]Pressure, error)
```
«За всё время» для пользователя по `user_id` (legacy-записи уже привязаны через `claimLegacy` в `doCmd` до роутинга — работает автоматически).

**`storage/sqlite/sqlite.go`** — реализация:
- `SELECT date, day_part, systolic, diastolic, heart_rate, user_id, user_name FROM blood_pressure WHERE user_id = ? ORDER BY date` (только параметризованный запрос).
- Дата в формате `2006-01-02` сортируется лексикографически = хронологически, `ORDER BY date` достаточно; порядок частей суток обеспечим в форматтере (см. п. 3).
- Ошибки по конвенции пакета — `fmt.Errorf("...: %w", err)`. Миграций не нужно — таблица и индексы уже есть.

## 2. Клиент Telegram

**`clients/telegram/telegram.go`**:
- Константа `sendDocumentMethod = "sendDocument"`.
- Новый метод `SendDocument(ctx context.Context, chatID int, filename string, data []byte) error`:
  - POST `multipart/form-data` на `sendDocument`: поле `chat_id`, файл `document` с `filename`.
  - Использовать `multipart.Writer` из stdlib.
  - Ответ проверить дважды, как требует AGENTS.md: HTTP-статус + поле `ok` → `toError()`; ошибки через `sanitize` (токен наружу не уходит). Тело ответа — та же форма `UpdatesResponse` (для ошибок важен только `ok/error_code/description/parameters`), можно переиспользовать `statusError`/`toError`.
  - Чтобы не дублировать обработку ответа, вынести общий хелпер (статус-чек + парсинг) из `doRequest`, либо добавить рядом отдельный `doMultipart`-путь; `sanitize`-обёртка остаётся в обоих.

`types.go` не меняется — поля `ok/error_code/...` уже есть.

## 3. Команды и форматирование

**`events/telegram/telegram.go`** — в интерфейс `Client` добавить:
```go
SendDocument(ctx context.Context, chatID int, filename string, data []byte) error
```
(интерфейс объявлен на стороне потребителя — конкретный `*clients/telegram.Client` удовлетворит его неявно; мок доработаем в тестах).

**`events/telegram/commands.go`**:
- Константа `DownloadCmd = "/download"`.
- В `doCmd` — `case DownloadCmd: return p.download(ctx, chatID, userID, username)`.
- Обработчик:
```go
func (p *Processor) download(ctx context.Context, chatID int, userID int64, username string) (err error) {
    // defer e.WrapIfErr("Ошибка при выполнении команды: download", err)
    // pressures, err := p.storage.GetAll(ctx, userID)
    //   err -> msgError + вернуть err
    // len == 0 -> p.tg.SendMessage(ctx, chatID, msgNoSavedPressure)
    // p.tg.SendDocument(ctx, chatID, csvFilename(username), []byte(formatCSV(pressures)))
}
```

**Новый файл `events/telegram/csv.go`** (CSV-генерация — бизнес-логика формата, живёт в `events/telegram`, как `formatPressures`):
- `formatCSV(pressures []storage.Pressure) string`:
  - Шапка из констант `messages.go` (см. п. 4).
  - Группировка по дате; для каждой даты фиксированный порядок частей суток: утро → день → вечер; пропущенные → пустые ячейки.
  - `strings.Join(fields, ";")`, CRLF (`\r\n`) и префикс UTF-8 BOM (`\xEF\xBB\xBF`) для корректного открытия в русском Excel.
- `csvFilename(username string) string` → `{username}_{timeloc.Now().Format(timeloc.DateFormat)}.csv`, при пустом username — `user`.

**`events/telegram/messages.go`** — константы (префикс `msg`, тесты будут сравнивать именно с ними):
- `msgCSVHeader` — полная строка шапки из формата выше.

## 4. Тесты

**`events/telegram/commands_test.go`** (дополняем существующий файл, table-driven стиль):
- Расширить `mockClient`: метод `SendDocument`, запоминающий `sentChatID`, `sentFilename`, `sentData`; добавить поле `sendDocErr`.
- Расширить `mockStorage`: поле `getAllFunc`, `getAllCalls`.
- `TestDownload_Empty` — `getAllFunc` возвращает `nil` → отправлено ровно `msgNoSavedPressure`, `SendDocument` не вызывался.
- `TestDownload_WithData` — данные за две даты → проверяем `sentFilename == "user_<today в Asia/Yekaterinburg>.csv"` и точное содержимое CSV (BOM + шапка + строки в порядке дат).
- `TestDownload_EmptyUsername` — `username == ""` → `sentFilename` начинается с `user_`.
- `TestDownload_StorageError` — sentinel-ошибка → `msgError` отправлен, ошибка пробрасывается и содержит префикс `"Ошибка при выполнении команды: download"` (аналог `TestSavePressure_StorageError`).
- `TestFormatCSV` — table-driven: одна дата с тремя частями суток; частичная (пустые ячейки); порядок дат и частей суток; корректность шапки (сравнение с `msgCSVHeader`).
- `TestCSVFilename` — формат имени и фолбэк `user`.
- В `TestDoCmd_Routing` добавить кейс для `/download` (проверка вызова `SendDocument`).
- Учесть: `doCmd` вызывает `claimLegacy` до роутинга — в тестах `mockStorage.ClaimLegacy` уже возвращает nil.

**`storage/sqlite/sqlite_test.go`**:
- `TestGetAll_AllTime` — записи за разные даты (включая не-сегодняшнюю) → возвращаются все, упорядочены по дате.
- `TestGetAll_ByUser` — записи двух пользователей → только свои (аналог `TestShow_ByUserID`).
- `TestGetAll_Empty` — пустая выборка, без ошибки.

**`clients/telegram`** (опционально, в репозитории сейчас нет тестов клиента): `httptest.Server`, проверяющий, что `SendDocument` шлёт POST на `/sendDocument`, multipart содержит `chat_id` и файл с правильным `filename`. Если решите не добавлять — не критично, покрытие команды закрывается моками выше.

## 5. Верификация

Перед завершением (по AGENTS.md): `go build ./...` → `go vet ./...` → `gofmt -l .` → `make test`. Коммит — только по явному запросу.

## Затрагиваемые файлы

| Файл | Изменение |
|---|---|
| `storage/storage.go` | + `GetAll` в интерфейс |
| `storage/sqlite/sqlite.go` | + реализация `GetAll` |
| `clients/telegram/telegram.go` | + `sendDocumentMethod`, + `SendDocument`, хелпер обработки ответа |
| `events/telegram/telegram.go` | + `SendDocument` в интерфейс `Client` |
| `events/telegram/commands.go` | + `DownloadCmd`, кейс роутинга, `download` |
| `events/telegram/csv.go` | **новый** — `formatCSV`, `csvFilename` |
| `events/telegram/messages.go` | + `msgCSVHeader` |
| `events/telegram/commands_test.go` | моки + тесты команды и формата |
| `storage/sqlite/sqlite_test.go` | тесты `GetAll` |
