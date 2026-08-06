# Правила тестирования

Запуск: `make test` (включает `-race -cover -count=1`), точечно — `go test ./events/telegram -run TestDayPart -v`.

Ключевые правила:

- SQLite-тесты используют файл в `t.TempDir()`, а **не** `:memory:` — пул `*sql.DB` открывает несколько соединений, каждое со своей пустой in-memory БД (флаки).
- Хелпер `today(t)` в `sqlite_test.go` берёт дату через `timeloc.Today()` — тест не зависит от фактической таймзоны прода.
- В `notifier/notifier_test.go` времена строятся через `time.Date(..., timeloc.Location())`, как в `nextTrigger(now, loc)`; персональные таймзоны тестируются через `time.FixedZone`, а сдвиги пользователей в `TestNotify_PerUserTimezones` задаются относительно серверной таймзоны (`serverOff`), поэтому смена таймзоны в проде не ломает тесты. Для детерминизма по времени у `Notifier` есть поле `now func() time.Time` (по умолчанию `time.Now`), которое тесты переопределяют вместо двойного вызова `time.Now()` (флак на границе срабатывания).
- Новые тесты пишутся в существующие файлы, в том же table-driven стиле.
