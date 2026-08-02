# План: CI через GitHub Actions

## Контекст
- Go 1.26.5, cgo-зависимый `go-sqlite3` → тесты требуют `CGO_ENABLED=1` (runners Ubuntu уже имеют gcc, доп. шаги не нужны).
- Тесты не используют `TG_KEY` (моки + CLI-режим без токена) → **секреты в CI не нужны**.
- Текущая верификация: `go build ./...` → `go vet ./...` → `gofmt -l .` → `make test`.

## Изменения

**1. Новый файл `.github/workflows/ci.yml`**

Триггеры: `push` (все ветки) + `pull_request`.

Две работы (параллельно, по одной на пул раннеров):

**Job `test`** (ubuntu-latest):
1. `actions/checkout@v4`
2. `actions/setup-go@v5` с `go-version-file: go.mod`, `cache: true` (кэш модулей `go-sqlite3` + зависимости)
3. `go build ./...`
4. `go vet ./...`
5. `test -z "$(gofmt -l .)"` — фейл при неотформатированных файлах
6. `make test` (`go test ./... -count=1 -race -cover`)

**Job `lint`** (ubuntu-latest):
1. checkout + setup-go (как выше)
2. `golangci-lint-action@v6` с `version: latest`
3. Линтер читает новый конфиг `.golangci.yml`; ошибки аннотируются в PR через reviewdog

**2. Новый файл `.golangci.yml`** — минимальный конфиг, согласованный с конвенциями проекта (ошибки через `lib/e`, package-level `regexp` и т.д.):
```yaml
run:
  timeout: 5m
linters:
  enable:
    - errcheck
    - govet
    - staticcheck
    - ineffassign
    - unused
    - gosimple
```
Линтер на старте может зацепить существующий код — в процессе подогнать код или конфиг так, чтобы локальный прогон был чистым.

**3. Makefile** — таргет `lint:` (тот же набор, что в CI): `golangci-lint run ./...`.

**4. README.md** (обязательно по AGENTS.md) — обновить строку «Линтера (golangci-lint) и CI в репозитории нет» → описать CI и `make lint`, добавить в раздел команд.

## Порядок работ
1. Создать `.golangci.yml`, установить golangci-lint локально и добиться чистого прогона на текущем коде (правки по мелочам).
2. Добавить `make lint`.
3. Создать `.github/workflows/ci.yml`.
4. Обновить README.
5. Локальная верификация: `go build ./...` → `go vet ./...` → `gofmt -l .` → `make test` → `make lint`.
6. Коммит/пуш — только по явному запросу.

## После пуша (по желанию, вручную)
- В GitHub: `Settings → Branches` → правило для `main` — required status checks `test` / `lint`.
