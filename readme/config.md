# Конфигурация

Все настройки — через переменные окружения с дефолтами (подходят для docker-compose):

| Переменная | Дефолт | Назначение |
|---|---|---|
| `TG_KEY` | — (обязателен для бота) | токен Telegram Bot API, только из окружения |
| `BP_DB_PATH` | `data/sqlite/storage.db` | путь к файлу БД (для бота и CLI) |
| `BP_TG_HOST` | `api.telegram.org` | хост Bot API |
| `BP_BATCH_SIZE` | `100` | размер пачки `getUpdates` |

Путь к БД в `docker-compose.yml` указывать не нужно: дефолт `data/sqlite/storage.db` совпадает с монтируемым volume `/app/data/sqlite`. При активном боте для CLI указывайте тот же путь, что и у бота (например, `BP_DB_PATH=... go run . export`).
