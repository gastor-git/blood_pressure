# blood-pressure-bot

Telegram бот для записи показаний артериального давления.

## Команды

```sh
dc_up        # docker compose up -d --remove-orphans
dc_down      # полная остановка + удаление volume/образов
dc_logs      # хвост логов
dc_build     # пересобрать
# Makefile не содержит test/lint — нет CI
```

## Запуск вне Docker

```sh
export TG_KEY="bot_token"
go run .
```

## Архитектура

- `main.go` — сборка зависимостей
- `events/telegram/` — `Processor` реализует оба интерфейса `events.Fetcher` + `events.Processor`, передаётся дважды: `event_consumer.New(eventsProcessor, eventsProcessor, batchSize)`
- `consumer/event-consumer/` — цикл fetch → process, `time.Sleep(1s)` при пустом ответе (нет long polling timeout)
- `clients/telegram/` — HTTP-клиент к Telegram Bot API, `http.Client{}` без таймаута
- `storage/sqlite/` — SQLite, требует CGO (`mattn/go-sqlite3`)
- `lib/e/` — утилиты `Wrap`/`WrapIfErr` для обёртки ошибок

## Примечания

- **Тестов нет.** `tests_plan.md` описывает детальный план, но код не реализован. Состояние: нулевое покрытие.
- `Consumer.Start()` — value receiver, `handleEvents` — pointer receiver (неконсистентно, описано в `PLAN.md`)
- Timezone `Asia/Yekaterinburg` хардкодом в `commands.go` и `sqlite.go`
- `ErrNoSavedPages` в `storage/storage.go` — мёртвый код (определён, нигде не используется)
- `context.Background()`/`context.TODO()` везде — нет таймаутов на контекст
- `.env` в `.gitignore` (содержит `TG_KEY`), копия без секретов — `.env.dist`
- go 1.25, модуль `blood-pressure-bot`

## Конвенции

- Ошибки оборачиваются через `lib/e.Wrap("msg", err)` / `e.WrapIfErr("msg", err)` с помощью defer
- Сообщения пользователю — в `events/telegram/messages.go`, константы с префиксом `msg`
- Telegram API-методы — константы в `clients/telegram/telegram.go`: `getUpdatesMethod`, `sendMessageMethod`
