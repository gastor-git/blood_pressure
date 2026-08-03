# Правила тестирования

Запуск: `make test` (включает `-race -cover -count=1`), точечно — `go test ./events/telegram -run TestDayPart -v`.

Ключевые правила:

- SQLite-тесты используют файл в `t.TempDir()`, а **не** `:memory:` — пул `*sql.DB` открывает несколько соединений, каждое со своей пустой in-memory БД (флаки).
- Хелпер `today(t)` в `sqlite_test.go` берёт дату в `Asia/Yekaterinburg` для вызовов `Show(ctx, id, date)`. Смена таймзоны в проде ломает тест.
- В `notifier/notifier_test.go` времена строятся через `time.Date(..., timeloc.Location())`, как в `nextTrigger(now, loc)`; персональные таймзоны тестируются через `time.FixedZone` с фиксированными UTC-моментами. Смена таймзоны в проде ломает тест (аналог правила выше).
- Новые тесты пишутся в существующие файлы, в том же table-driven стиле.
