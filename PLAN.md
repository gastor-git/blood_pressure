# План: персональная таймзона пользователя

Задача из IMPROVEMENTS.md: определять текущее время пользователя (и текущее время суток) из данных клиента Telegram (часовой пояс) и при записи данных в БД использовать время пользователя.

Объём (согласовано): запись + `/show` + персональный `notifier`, хранение пояса в БД.

## Суть

Telegram отдаёт часовой пояс только через `getChat` → `ChatFullInfo.utc_offset` (секунды от UTC, для личных чатов, `0` = неизвестно). Запрос кэшируется (как `claimed`), результат пишется в БД (`users.utc_offset`, миграция 4). Все вычисления «сегодня / часть суток» при записи, в `/show` и в `notifier` идут в локали пользователя; fallback — `lib/timeloc` (серверная `Asia/Yekaterinburg`), если offset неизвестен (`0` или ошибка `getChat`).

## Шаги

### 1. `clients/telegram` — метод `GetChat`

- `types.go`: тип `ChatFullInfo{ID int64; UTCOffset int}` (json `utc_offset`) + обёртка ответа `GetChatResponse` (Ok/ErrorCode/Description/Parameters/Result `*ChatFullInfo`), `toError` по образцу `UpdatesResponse`.
- `telegram.go`: константа `getChatMethod = "getChat"`, метод `GetChat(ctx, chatID int) (*ChatFullInfo, error)` через `doRequest` + проверка `ok`.
- `telegram_test.go`: `TestGetChat` (путь, `chat_id`, распарсенный offset), `TestGetChat_OKFalse` (APIError).

### 2. `storage` — интерфейс, миграция 4, реализация

- `storage.go`:
  - `User` += `UTCOffset int`.
  - `Show(ctx, userID int64, date string)` — дата передаётся явно (вместо внутреннего `timeloc.Now()`).
  - `UsersWithoutPressure` удалить.
  - Добавить `SetUTCOffset(ctx, userID int64, offset int) error` и `AllUsers(ctx) ([]User, error)`.
- `sqlite/migrations.go`: `migration4` — `ALTER TABLE users ADD COLUMN utc_offset INTEGER NOT NULL DEFAULT 0`; добавить в срез.
- `sqlite/sqlite.go`: `Show` фильтрует по переданной дате; `AllUsers` (`SELECT user_id, chat_id, user_name, utc_offset ORDER BY user_id`); `SetUTCOffset` (`UPDATE users SET utc_offset = ? WHERE user_id = ?`); удалить `UsersWithoutPressure`. `RegisterUser` не меняется (offset 0 по DEFAULT, заполняется `SetUTCOffset`).
- `sqlite/sqlite_test.go`: обновить вызовы `Show(ctx, id, date)` (передавать `today(t)`); заменить `TestUsersWithoutPressure*` и использования в `TestRegisterUser_*` на `TestAllUsers` и `TestSetUTCOffset`; проверить миграцию 4 (версия `len(migrations)`, колонка `utc_offset`).

### 3. `events/telegram` — кэш offset + применение при записи и `/show`

- `telegram.go`: в интерфейс `Client` добавить `GetChat(ctx, chatID int) (*tgClient.ChatFullInfo, error)`; в `Processor` поле `utcOffsets map[int64]int` (без мьютекса, консьюмер однопоточный), инициализация в `New`.
- Новый `userloc.go`:
  - `ensureTimezone(ctx, chatID, userID)` — если offset в кэше, выйти; иначе `GetChat`, закэшировать (даже `0`), при `!=0` — `SetUTCOffset`; при ошибке — `log` (санитизированная ошибка клиента) и не кэшировать (повтор на следующем сообщении).
  - `userLoc(userID) *time.Location` — `time.FixedZone("", off)` при `off != 0`, иначе `timeloc.Location()`.
  - `userNow(ctx, chatID, userID) time.Time` = `time.Now().In(userLoc(...))` (после `ensureTimezone`).
  - `userToday(...) string` — `userNow().Format(timeloc.DateFormat)`.
- `commands.go`:
  - `doCmd`: после `RegisterUser` вызывать `p.ensureTimezone(ctx, chatID, userID)` (нужно для персиста пояса, даже если пользователь только смотрит `/show`).
  - `savePressure`: `now := p.userNow(ctx, chatID, userID)` вместо `timeloc.Now()`.
  - `show`: `p.storage.Show(ctx, userID, p.userToday(ctx, chatID, userID))`.
  - `download` — без изменений (вне задачи).
- `dialog.go`: `sendDatePrompt`/`sendDayPartPrompt`/`handleDate` получают `userID` и используют `userNow`/`userToday` (метка «Сегодня», проверка будущей даты, подсказка части суток). `startAdd`/`handleDialog` прокидывают `userID`.
- `commands_test.go`:
  - `mockClient.GetChat` (по умолчанию `UTCOffset: 0` → fallback, старые тесты не ломаются; считать вызовы), `mockStorage.Show(ctx, id, date)`, добавить `SetUTCOffset`/`AllUsers`.
  - Новые тесты: кэш `GetChat` вызывается 1 раз; запись с большим offset (напр. `+14h`) сохраняет локальную дату/часть суток пользователя; `/show` передаёт локальную дату; fallback при ошибке/нулевом offset.

### 4. `notifier` — персональные пояса

- `notifier.go`:
  - Интерфейс `Storage`: `AllUsers(ctx)` + `Get(ctx, userID, date, dayPart)` вместо `UsersWithoutPressure`.
  - `nextTrigger(now time.Time, loc *time.Location)` — параметризовать локацию.
  - `userLoc(offset int) *time.Location` (offset 0 → `timeloc.Location()`); `isReminderMinute(t)` — час из `reminderHours` и минута `reminderMinute`.
  - `Start`: `AllUsers` → мин. `nextTrigger` по всем пользователям (fallback серверная ТЗ при пустом списке/ошибке) → таймер → `notify(ctx, time.Now())`.
  - `notify(ctx, now)`: для каждого пользователя локальное время; если `isReminderMinute` — дата = локальный день, метка = `telegram.DayPart(localNow)`; если `Get` вернул `nil` (нет записи) — напоминание `MsgReminder`.
- `notifier_test.go`: `nextTrigger(c.now, timeloc.Location())`; mockStorage `AllUsers`/`Get`; `TestNotify_PerUserTimezones` (разные offsets — напоминания только тем, у кого локально наступил триггер), `TestNotify_AlreadyRecorded` (Get вернул запись — пропуск), `TestIsReminderMinute`; обновить `TestNotify` и тесты ошибок.

### 5. `README.md`

- Обновить таблицу структуры (`GetChat` в клиенте, `userloc.go`, `Show(date)`, `AllUsers`/`SetUTCOffset`, миграция 4, персональный notifier), раздел стиля (таймзона: `getChat.utc_offset` + fallback на `timeloc`; `utc_offset == 0` трактуется как неизвестный → серверная ТЗ; ограничение — реальные UTC-пользователи получат серверную ТЗ), правила тестирования.

### 6. Верификация

`go build ./...` → `go vet ./...` → `gofmt -l .` → `make test`.

## Открытые моменты / границы

- `utc_offset == 0` неразличим: «реальный UTC» и «неизвестно» → fallback на серверную ТЗ (ограничение API).
- Кэш `utcOffsets` живёт до рестарта; offset может устареть при смене DST/перемещении — допустимо, перечитывается при следующем рестарте (и сохраняется в БД).
- `/download` (имя файла) остаётся на серверной ТЗ — вне задачи.
