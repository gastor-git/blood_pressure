# План тестов — только критический функционал

Состояние: покрытие 0%. Цель — покрыть парсинг входных данных, дедупликацию и
персистентность. Ассерты — stdlib (`reflect.DeepEqual` + `t.Errorf`), table-driven,
без новых зависимостей в `go.mod`.

## Что считаем критичным

| Функционал | Где | Почему |
|---|---|---|
| Парсинг показаний | `events/telegram/commands.go:122-135` | Единственный вход данных; ошибка = потеря/искажение записи |
| Определение части суток | `events/telegram/commands.go:53-62` | Определяет ключ дедупликации |
| Защита от дублей | `events/telegram/commands.go:75-81` | Бизнес-правило «одна запись на часть суток» |
| Персистентность | `storage/sqlite/sqlite.go` — `Save`/`IsExists`/`Show` | Данные пользователя |
| Роутинг команд | `events/telegram/commands.go:21-40` | Точка ветвления всего поведения |

## Осознанно вне скоупа

- `lib/e` — 8 строк обёртки над `fmt.Errorf`, тест ничего не ловит
- `consumer/event-consumer` — бесконечный `for` с `log.Printf`; требует горутин и каналов, ценность низкая
- `clients/telegram` — тонкий HTTP-враппер, `httptest` проверял бы в основном stdlib
- `storage.Remove` — из бота не вызывается
- `Fetch` / `Process` / `meta` / `event` — механическая конвертация типов
- Фикс бага `dayPart` и валидация диапазонов показаний — отдельные задачи

---

## Шаг 1. Правки прод-кода (минимальные, ~15 строк)

### `events/telegram/telegram.go`

`Processor.tg` имеет конкретный тип `*telegram.Client` — замокать `SendMessage`
невозможно. Вводим consumer-side интерфейс:

```go
type Client interface {
    Updates(offset, limit int) ([]telegram.Update, error)
    SendMessage(chatID int, text string) error
}
```

- поле `tg Client` вместо `tg *telegram.Client`
- `New(client Client, storage storage.Storage) *Processor`
- `main.go` **не меняется** — `*tgClient.Client` удовлетворяет интерфейсу неявно

### `events/telegram/commands.go`

Извлечь из `savePressure` (строки 53-62) чистую функцию:

```go
func dayPart(t time.Time) string
```

Логика переносится 1:1, включая `hour > 0`. DI-поле `now func() time.Time`
в `Processor` **не добавляем** — чистая функция дешевле и покрывает границы полностью.

---

## Шаг 2. `events/telegram/commands_test.go`

### Моки (в тестовом файле)

```go
type mockClient struct {
    sentChatID int
    sentTexts  []string
    sendErr    error
}

type mockStorage struct {
    saveFunc     func(ctx context.Context, p *storage.Pressure) error
    showFunc     func(ctx context.Context, userName string) (string, error)
    isExistsFunc func(ctx context.Context, p *storage.Pressure) (bool, error)

    saveCalls     int
    isExistsCalls int
}
```

### Тесты

| Тест | Кейсы |
|---|---|
| `TestIsPressure` | валид: `120 80 70`, `90 60 50`, `100 100 100`, `999 999 999` (диапазоны не валидируются — фиксируем текущее поведение)<br>невалид: `120 80`, `120 80 70 60`, `1 2 3`, `1200 80 70`, `120  80 70` (двойной пробел), `abc`, `""` |
| `TestGetPressures` | `120 80 70` → `["120","80","70"]` |
| `TestDayPart` | 00:00→вечер, 00:59→вечер, 01:00→утро, 12:00→утро, 12:01→день, 18:00→день, 18:01→вечер, 23:59→вечер |
| `TestDoCmd_Routing` | `/help`→`msgHelp`, `/start`→`msgHello`, `/foo`→`msgUnknownCommand`, `"  /help  "`→`msgHelp` (проверка `TrimSpace`) |
| `TestSavePressure_New` | `IsExists=false` → `saveCalls == 1`, поля `Pressure` корректны, отправлен `msgSaved` |
| `TestSavePressure_Duplicate` | `IsExists=true` → `saveCalls == 0`, отправлен `msgAlreadyExists` |
| `TestSavePressure_StorageError` | ошибка `Save` пробрасывается наружу, префикс обёртки присутствует |
| `TestShow_Empty` | `Show` → `("", nil)` → отправлен `msgNoSavedPressure` |
| `TestShow_WithData` | `Show` → непустая строка → она же отправлена пользователю |

> Примечание: реальный `sqlite.Show` никогда не возвращает `ErrNoSavedPressure`
> (`db.Query` не отдаёт `sql.ErrNoRows`). Рабочая ветка в `commands.go:103` —
> только `msg == ""`. Тест это фиксирует.

---

## Шаг 3. `storage/sqlite/sqlite_test.go`

Helper:

```go
func newTestStorage(t *testing.T) *Storage // filepath.Join(t.TempDir(), "test.db") + Init + t.Cleanup
```

Файл в `t.TempDir()`, а **не** `:memory:` — пул `*sql.DB` может открыть несколько
соединений, каждое со своей пустой in-memory БД, что даёт флаки.

| Тест | Что проверяет |
|---|---|
| `TestInit_Idempotent` | двойной `Init` без ошибки |
| `TestSave_IsExists` | сохранить → `true`; другой `day_part` → `false`; другой `user_name` → `false` |
| `TestShow_Today` | запись за сегодня попадает в вывод, формат строки соответствует ожидаемому |
| `TestShow_OtherDay` | запись с датой вчера → пустая строка (фильтр по дате работает) |
| `TestShow_Empty` | пустая БД → `("", nil)`, а не `ErrNoSavedPressure` |

Требует CGO (`mattn/go-sqlite3`) — в окружении есть `gcc`, `CGO_ENABLED=1`.

---

## Шаг 4. Makefile

```make
##################
# Tests
##################

test:
	go test ./... -count=1 -race -cover
```

---

## Верификация

```sh
go build ./...   # сборка не сломана
go vet ./...     # чисто
make test        # все тесты зелёные
```

## Порядок реализации

1. Правки прод-кода (интерфейс `Client`, функция `dayPart`) + `go build ./...`
2. `events/telegram/commands_test.go` — основная бизнес-логика
3. `storage/sqlite/sqlite_test.go` — персистентность
4. `Makefile` target `test`

Итого: 2 тестовых файла, ~14 тест-функций.
