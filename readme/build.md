# Команды сборки и разработки

```sh
go build ./...                                 # сборка
go vet ./...                                   # должен быть чистым
gofmt -l .                                     # должен выводить пустой список
make test                                      # go test ./... -count=1 -race -cover
make lint                                      # golangci-lint run ./... (golangci-lint v2)
go test ./events/telegram -run TestDayPart -v  # один тест
go test ./notifier -run TestNextTrigger -v     # один тест пакета notifier
TG_KEY="token" go run .                        # запуск вне Docker
```

Docker:

```sh
make dc_build          # сборка образа
make dc_up             # up -d --remove-orphans
make dc_ps / dc_logs   # статус / логи
make dc_stop           # остановить
make dc_down           # ВНИМАНИЕ: сносит volume (-v) и образы (--rmi=all)
```

CLI для управления БД (запускается без `TG_KEY`, ходит в ту же БД, что и бот):

```sh
go run . help                                                # справка
make cli_help
go run . export                                              # все записи в export_<сегодня>.csv
make cli_export ARGS="-user-name alice -from 01.01.2026"     # фильтр по user_name + диапазон дат
make cli_export ARGS="-user-id 7 -out /tmp/export.csv"
go run . delete -yes                                         # ВНИМАНИЕ: удаляет ВСЕ записи
make cli_delete ARGS="-yes"
make cli_delete ARGS="-user-name bob -from 01.01.2026 -to 31.01.2026 -yes"
go run . backup                                              # копия БД в backup_<сегодня>.db
go run . backup -out /tmp/backup.db                          # копия в указанный путь
make cli_backup ARGS="-out /tmp/backup.db"
go run . health                                              # проверка доступности БД (для healthcheck)
make cli_health
```

Резервное копирование: `backup` создаёт консистентную копию через `VACUUM INTO` и безопасен на живом боте (WAL). Файл назначения не должен существовать; кладите бэкап **вне** каталога `data/sqlite`. Для регулярного копирования запускайте по cron, например: `30 3 * * * cd /путь/к/проекту && go run . backup -out /mnt/backups/backup_$(date +\%F).db`.

Формат CSV: UTF-8 с BOM, разделитель `;`, одна строка на (пользователь, дата), отсутствующие части суток — пустые ячейки. Шапка: `User_ID;User_name;Дата;Утро;День;Вечер`. Для `delete` без `--yes` — отказ с предупреждением; без фильтров команда удаляет все записи. При активном боте (WAL, `_busy_timeout=5000`) возможна задержка записи до 5с; для `delete` безопаснее останавливать бот.
