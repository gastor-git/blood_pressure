package sqlite

import (
	"context"
	"database/sql"
	"fmt"
)

// migrations — упорядоченный список миграций схемы. Индекс + 1 соответствует
// целевой версии схемы (PRAGMA user_version). Добавлять новые миграции можно
// только в конец: порядок и номера версий фиксированы.
var migrations = []func(context.Context, *sql.Tx) error{
	migration1,
	migration2,
	migration3,
	migration4,
}

// migrate приводит схему к последней версии. Текущая версия хранится в
// PRAGMA user_version; применяются только недостающие миграции, каждая — в
// своей транзакции, после чего версия поднимается.
func (s *Storage) migrate(ctx context.Context) error {
	version, err := s.schemaVersion(ctx)
	if err != nil {
		return err
	}

	for i := version; i < len(migrations); i++ {
		if err := s.applyMigration(ctx, i); err != nil {
			return fmt.Errorf("can't apply migration %d: %w", i+1, err)
		}
	}

	return nil
}

// schemaVersion читает текущую версию схемы.
func (s *Storage) schemaVersion(ctx context.Context) (int, error) {
	var version int
	if err := s.db.QueryRowContext(ctx, `PRAGMA user_version`).Scan(&version); err != nil {
		return 0, fmt.Errorf("can't read schema version: %w", err)
	}

	return version, nil
}

// applyMigration выполняет миграцию с индексом idx в отдельной транзакции и
// поднимает user_version до idx+1.
func (s *Storage) applyMigration(ctx context.Context, idx int) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("can't begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if err := migrations[idx](ctx, tx); err != nil {
		return err
	}

	// PRAGMA user_version не поддерживает плейсхолдеры (?), поэтому номер
	// подставляется в строку. Это осознанное отступление от правила
	// «только параметризованный SQL»: значение — int-константа из кода
	// (idx+1), а не пользовательский ввод, инъекция невозможна.
	if _, err := tx.ExecContext(ctx, fmt.Sprintf(`PRAGMA user_version = %d`, idx+1)); err != nil {
		return fmt.Errorf("can't bump schema version: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("can't commit tx: %w", err)
	}

	return nil
}

// migration1 — исходная схема таблицы показаний.
func migration1(ctx context.Context, tx *sql.Tx) error {
	q := `CREATE TABLE IF NOT EXISTS blood_pressure (date TEXT, day_part TEXT, systolic TEXT, diastolic TEXT, heart_rate TEXT, user_name TEXT)`

	if _, err := tx.ExecContext(ctx, q); err != nil {
		return fmt.Errorf("can't create table: %w", err)
	}

	return nil
}

// migration2 — переход на ключ user_id: добавление колонки, страховочная
// дедупликация и уникальный индекс по (date, day_part, user_id) плюс частичный
// индекс под ленивый backfill legacy-строк (user_id IS NULL).
func migration2(ctx context.Context, tx *sql.Tx) error {
	stmts := []string{
		`ALTER TABLE blood_pressure ADD COLUMN user_id INTEGER`,
		`DELETE FROM blood_pressure WHERE rowid NOT IN (SELECT MIN(rowid) FROM blood_pressure GROUP BY date, day_part, user_name)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_pressure_key ON blood_pressure(date, day_part, user_id)`,
		`CREATE INDEX IF NOT EXISTS idx_pressure_legacy ON blood_pressure(user_name) WHERE user_id IS NULL`,
	}

	for _, q := range stmts {
		if _, err := tx.ExecContext(ctx, q); err != nil {
			return fmt.Errorf("can't run statement %q: %w", q, err)
		}
	}

	return nil
}

// migration3 — таблица пользователей для рассылки напоминаний. Заполняется
// upsert-ом при каждом входящем сообщении; на неё опирается notifier.
func migration3(ctx context.Context, tx *sql.Tx) error {
	q := `CREATE TABLE IF NOT EXISTS users (
		user_id INTEGER PRIMARY KEY,
		chat_id INTEGER NOT NULL,
		user_name TEXT,
		updated_at TEXT NOT NULL
	)`

	if _, err := tx.ExecContext(ctx, q); err != nil {
		return fmt.Errorf("can't create users table: %w", err)
	}

	return nil
}

// migration4 — персональная таймзона пользователя: utc_offset (секунды от UTC)
// из getChat. 0 = неизвестно (или реальный UTC) → fallback на серверную таймзону.
func migration4(ctx context.Context, tx *sql.Tx) error {
	q := `ALTER TABLE users ADD COLUMN utc_offset INTEGER NOT NULL DEFAULT 0`

	if _, err := tx.ExecContext(ctx, q); err != nil {
		return fmt.Errorf("can't add utc_offset column: %w", err)
	}

	return nil
}
