# План: перезапись показаний при дубликате

## Анализ текущего поведения

Обе точки ввода — быстрый `savePressure` (`events/telegram/commands.go:164`) и диалог `/add` `completeDialog` (`events/telegram/dialog.go:144`) — при дубликате по ключу `(date, day_part, user_id)` вызывают `save` → `storage.Save` (INSERT ON CONFLICT DO NOTHING) → при `saveDuplicate` показывают только `msgAlreadyExists` (`events/telegram/messages.go:17`). Записи нет ни `Get`-а, ни `Update`-а в `storage.Storage`.

## Изменения

### 1. `storage/storage.go` — расширить интерфейс
Добавить два метода (с doc-комментариями):
- `Get(ctx, userID, date, dayPart) (*Pressure, error)` — запись по ключу; `(nil, nil)` если нет.
- `Update(ctx, p *Pressure) error` — перезапись значений существующей записи.

### 2. `storage/sqlite/sqlite.go` — реализация
- `Get`: `SELECT ... WHERE user_id = ? AND date = ? AND day_part = ?`, `sql.ErrNoRows` → `(nil, nil)`.
- `Update`: `UPDATE blood_pressure SET systolic = ?, diastolic = ?, heart_rate = ?, user_name = ? WHERE user_id = ? AND date = ? AND day_part = ?`; при `RowsAffected() == 0` — ошибка. Параметризованный SQL, ошибки через `fmt.Errorf` (как в пакете).

### 3. `events/telegram/messages.go` — тексты
- Удалить `msgAlreadyExists` (заменяется новым флоу).
- Добавить: `msgDuplicatePrompt` (шаблон: дата `formatCSVDate`, часть суток, ранее введённые `sys/dia/hr`, новые значения), `msgOverwriteButton` / `msgKeepButton`, `msgOverwritten`, `msgKeepExisting`, `msgInvalidOverwriteChoice`.

### 4. `events/telegram/dialog.go` — state-machine
- `dialogState`: добавить `stateConfirmOverwrite`.
- `session`: добавить `pendingSys / pendingDia / pendingHr string`.
- `overwriteKeyboard = [][]string{{msgOverwriteButton}, {msgKeepButton}}`.
- `handleDialog`: роутинг `case stateConfirmOverwrite → handleOverwrite`.
- `handleOverwrite`: «Перезаписать» → `doOverwrite`; «Не перезаписывать» → `RemoveKeyboard(msgKeepExisting)` + `cancelSession`; любой другой ввод → повторный промпт `msgInvalidOverwriteChoice`.
- `doOverwrite`: defensive-валидация отложенных значений через `validatePressure` (в т.ч. повторная по требованию «при перезаписи осуществить валидацию») → `storage.Update` → `RemoveKeyboard(msgOverwritten)` + `cancelSession`; при ошибке БД — `msgError`.
- `completeDialog`: ветка `saveDuplicate` вместо `msgAlreadyExists` → `confirmOverwrite` (сессию НЕ отменять); на `saveFailed`/ошибке — `cancelSession` как сейчас.

### 5. `events/telegram/commands.go` — быстрый ввод
- `savePressure`: ветка `saveDuplicate` → `confirmOverwrite` (вместо `msgAlreadyExists`).
- Новый `confirmOverwrite(ctx, chatID, userID, date, dayPart, username, sys, dia, hr)`:
  1. `storage.Get` существующей записи (ошибка → `msgError`);
  2. переиспользовать активную сессию (диалог) или создать новую (быстрый ввод), установить `stateConfirmOverwrite` + отложенные значения;
  3. `SendKeyboard` с `msgDuplicatePrompt` (дата/часть суток/старые показания/новые).

### 6. Тесты
- `events/telegram/commands_test.go`:
  - `mockStorage`: добавить `getFunc`/`updateFunc` и счётчики;
  - обновить `TestSavePressure_Duplicate` и `TestAddDialog_Duplicate` (теперь ждут промпт + keyboard + сессию в `stateConfirmOverwrite`, не `msgAlreadyExists`);
  - новые: подтверждение перезаписи (быстрый + диалог), отказ, невалидный ответ-повторный промпт, ошибки `Get`/`Update`;
- `storage/sqlite/sqlite_test.go`: `TestGet_*` (найдено / нет записи / другой пользователь), `TestUpdate_*` (перезапись, запись не найдена).

### 7. `README.md`
Обновить описания `storage.Storage`, `sqlite.go`, `dialog.go`, `commands.go` и тексты в `messages.go` под новый флоу (требование AGENTS.md).

## Замечания
- Гонок нет: консьюмер однопоточный, notifier только читает; `ON CONFLICT DO NOTHING` остаётся страховкой.
- Сессия подтверждения перехватывает не-командные сообщения; любая команда отменяет её (существующая логика `doCmd`).
- Именованные возвращаемые значения + `defer e.WrapIfErr(...)` в новых функциях, тексты только в `messages.go`.

## Верификация
`go build ./...` → `go vet ./...` → `gofmt -l .` → `make test`
