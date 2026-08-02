# Настройка CI в GitHub

CI реализован через GitHub Actions и не требует создания workflow на стороне GitHub — он уже лежит в репозитории: `.github/workflows/ci.yml`. Нужно только убедиться, что Actions включены, и запустить.

## Как это работает

Workflow `CI` срабатывает на:

- `push` — во все ветки;
- `pull_request` — на создание/обновление PR.

Две работы выполняются параллельно на `ubuntu-latest`:

| Job | Что делает |
|---|---|
| `test` | `go build ./...` → `go vet ./...` → `gofmt -l .` (фейл при неотформатированных файлах) → `make test` (`go test ./... -count=1 -race -cover`) |
| `lint` | `golangci-lint run ./...` (версия `v2.12` берётся из `ci.yml`, конфиг — `.golangci.yml`) |

Секреты и переменные окружения не нужны: тесты не используют `TG_KEY` (моки + CLI-режим без токена), `go-sqlite3` собирается штатно с cgo на раннерах Ubuntu.

## Порядок действий

1. **Убедиться, что Actions включены.**
   `Settings → Actions → General`:
   - `Actions permissions` → `Allow all actions and reusable workflows` (или вручную разрешить `actions/checkout`, `actions/setup-go`, `golangci/golangci-lint-action`).

2. **Запустить workflow.**
   Вкладка `Actions` → workflow `CI` → `Run workflow`, либо просто сделать пуш в любую ветку.

3. **Проверить результат.**
   Во вкладке `Actions` видны запуски `push` и `pull_request`; каждый содержит работы `test` и `lint`. Если шаг упал — в логах сразу видно причину: неотформатированный файл, ошибка `vet`, упавший тест или замечание линтера. Замечания golangci-lint в PR аннотируются прямо в diff.

4. **Локальная проверка перед пушем.**
   ```sh
   go build ./...
   go vet ./...
   test -z "$(gofmt -l .)"
   make test
   make lint
   ```
