# План: напоминания о передаче показаний АД

Бот уведомляет пользователя за 30 минут до окончания части суток (Утро, День, Вечер), что пора передать показания артериального давления. Текст: «Пора передать показания за {часть суток}».

## Решения

- Время напоминаний — в таймзоне учёта `lib/timeloc` (`Asia/Yekaterinburg`), за 30 минут до конца части суток: утро → **11:30**, день → **17:30**, вечер → **23:30**.
- Получатели — только пользователи, ещё **не** передавшие показания за текущую часть суток.
- Источник списка пользователей — новая таблица `users`, заполняется upsert-ом при каждом входящем сообщении (пользователи, ни разу не сохранявшие показания, тоже получают напоминания).
- Текст напоминания собирается из константы `msgReminder` (префикс `msg`, живёт в `events/telegram/messages.go` по конвенции) + метка части суток строчными (`утро`/`день`/`вечер`).
- Рассылка — отдельный пакет `notifier` с циклом `Start(ctx)` в собственной горутине; long-polling-консьюмер не трогаем. Интерфейсы (`Storage`, `Sender`) объявлены на стороне потребителя.
- Пропущенный триггер (например, процесс не был запущен в 11:30) не догоняется: при старте вычисляется ближайшее будущее время срабатывания. Осознанное ограничение.

## 1. Storage-слой

**`storage/storage.go`** — модель и новые методы интерфейса:
```go
type User struct {
	UserID   int64
	ChatID   int64
	UserName string
}
// RegisterUser — upsert пользователя при каждом входящем сообщении.
RegisterUser(ctx context.Context, userID int64, chatID int64, userName string) error
// UsersWithoutPressure — пользователи без записи за дату+часть суток.
UsersWithoutPressure(ctx context.Context, date, dayPart string) ([]User, error)
```

**`storage/sqlite/migrations.go`** — `migration3` (добавляется в конец среза `migrations`):
```sql
CREATE TABLE IF NOT EXISTS users (
	user_id INTEGER PRIMARY KEY,
	chat_id INTEGER NOT NULL,
	user_name TEXT,
	updated_at TEXT NOT NULL
)
```

**`storage/sqlite/sqlite.go`**:
- `RegisterUser` — `INSERT INTO users (user_id, chat_id, user_name, updated_at) VALUES (?, ?, ?, ?) ON CONFLICT(user_id) DO UPDATE SET chat_id = excluded.chat_id, user_name = excluded.user_name, updated_at = excluded.updated_at`; `updated_at = timeloc.Now().Format(time.RFC3339)`. Только параметризованный запрос.
- `UsersWithoutPressure`:
  ```sql
  SELECT u.user_id, u.chat_id, u.user_name FROM users u
  WHERE NOT EXISTS (
      SELECT 1 FROM blood_pressure bp
      WHERE bp.user_id = u.user_id AND bp.date = ? AND bp.day_part = ?
  )
  ```
  Legacy-записи с `user_id IS NULL` не блокируют напоминание (не принадлежат зарегистрированному пользователю).
- Ошибки по конвенции пакета — `fmt.Errorf("...: %w", err)`.

## 2. Регистрация пользователя и текст

**`events/telegram/commands.go`**:
- В `doCmd` после `claimLegacy`:
  ```go
  if err := p.storage.RegisterUser(ctx, userID, int64(chatID), username); err != nil {
  	return e.Wrap("не удалось сохранить пользователя", err)
  }
  ```
- `dayPart` → экспорт в `DayPart(t time.Time) string` (нужен пакету `notifier` для метки части суток). Обновить вызов в `savePressure` и `TestDayPart`.

**`events/telegram/messages.go`** — `const msgReminder = "Пора передать показания за %s"` (единственное место текстов для пользователя).

## 3. Пакет `notifier` (новый)

**`notifier/notifier.go`**:
```go
type Storage interface {
	UsersWithoutPressure(ctx context.Context, date, dayPart string) ([]storage.User, error)
}

type Sender interface {
	SendMessage(ctx context.Context, chatID int, text string) error
}

type Notifier struct {
	storage Storage
	tg      Sender
}
```
- `var reminderHours = []int{11, 17, 23}` — часы срабатывания (минута всегда `30`).
- `nextTrigger(now time.Time) time.Time` — чистая функция: ближайшее из времён `{11:30, 17:30, 23:30}` строго после `now`; если все прошли — первое из них завтра. Построение времени — через `time.Date(..., timeloc.Location())`.
- `Start(ctx) error` — цикл: `nextTrigger(timeloc.Now())` → `time.NewTimer(time.Until(trigger))` + `select` на `ctx.Done()` → `notify`; ошибки логируются, цикл продолжается; завершается по отмене `ctx` (как консьюмер).
- `notify(ctx, trigger)` — метка `telegram.DayPart(trigger)`; `UsersWithoutPressure(ctx, timeloc.Today(), метка)`; каждому пользователю `SendMessage(ctx, int(u.ChatID), fmt.Sprintf(msgReminder, метка))`. Ошибка отправки одному пользователю не прерывает рассылку остальным. Ошибки — `lib/e` через `defer` (`WrapIfErr`). Логировать только санитизированные ошибки клиента.

## 4. `main.go`

Запуск рассылки в отдельной горутине рядом с консьюмером:
```go
go func() {
	if err := notifier.New(s, tgClient.New(tgBotHost, mustToken())).Start(ctx); err != nil {
		log.Print("notifier stopped: ", err)
	}
}()
```
SQLite-пул с `SetMaxOpenConns(1)` сериализует одновременные запросы консьюмера и нотификатора — гонок нет.

## 5. Тесты

**`events/telegram/commands_test.go`** (существующий файл, table-driven стиль):
- `mockStorage` дополнить методами `RegisterUser`/`UsersWithoutPressure` — интерфейс `storage.Storage` расширен, без них тесты не соберутся.
- `TestRegisterUser_CalledOnCommand` — каждый `doCmd` вызывает `RegisterUser` с корректными `userID`/`chatID`/`username`.
- `TestDayPart` — переименование вызова `dayPart` → `DayPart` (границы не меняются).

**`storage/sqlite/sqlite_test.go`**:
- `TestRegisterUser_Insert` — первый вызов создаёт запись.
- `TestRegisterUser_Upsert` — повторный вызов обновляет `chat_id`/`user_name`, не плодит дубликаты.
- `TestUsersWithoutPressure` — возвращает только тех, у кого нет записи за дату+часть суток; передавший исключён; legacy-строка с `user_id IS NULL` не мешает; пустая выборка без ошибки.

**`notifier/notifier_test.go`** (новый):
- `TestNextTrigger` — до 11:30 → 11:30 сегодня; 12:00 → 17:30; 18:00 → 23:30; 23:31 → 11:30 завтра; ровно 11:30 → 17:30 (триггер строго позже `now`).
- `TestNotify` — мок `Storage`/`Sender`: двум пользователям ушёл `fmt.Sprintf(msgReminder, "день")`, метка выведена из `trigger`.
- `TestNotify_StorageError` — ошибка хранилища обёрнута (`errors.Is` по sentinel), рассылка не выполнялась.
- `TestNotify_SendError` — сбой отправки одному пользователю: остальные получают.

## 6. Верификация

Перед завершением (по AGENTS.md): `go build ./...` → `go vet ./...` → `gofmt -l .` → `make test`. Коммит — только по явному запросу.

## Затрагиваемые файлы

| Файл | Изменение |
|---|---|
| `storage/storage.go` | + `User`, + `RegisterUser`, + `UsersWithoutPressure` |
| `storage/sqlite/migrations.go` | + `migration3` (таблица `users`) |
| `storage/sqlite/sqlite.go` | + реализации `RegisterUser`, `UsersWithoutPressure` |
| `events/telegram/commands.go` | + вызов `RegisterUser` в `doCmd`, `dayPart` → `DayPart` |
| `events/telegram/messages.go` | + `msgReminder` |
| `notifier/notifier.go` | **новый** — `Notifier`, `Start`, `nextTrigger`, `notify` |
| `main.go` | + запуск `notifier` в горутине |
| `events/telegram/commands_test.go` | моки + тест `RegisterUser` |
| `storage/sqlite/sqlite_test.go` | тесты `RegisterUser`, `UsersWithoutPressure` |
| `notifier/notifier_test.go` | **новый** — тесты расписания и рассылки |
