package sqlite

import (
	"context"
	"database/sql"
	"fmt"

	_ "github.com/mattn/go-sqlite3"

	"blood-pressure-bot/lib/timeloc"
	"blood-pressure-bot/storage"
)

type Storage struct {
	db *sql.DB
}

// New creates new SQLite storage.
func New(path string) (*Storage, error) {
	dsn := path + "?_busy_timeout=5000&_journal_mode=WAL"

	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, fmt.Errorf("can't open database: %w", err)
	}

	// go-sqlite3 не поддерживает конкурентную запись; один коннект убирает
	// гонки за файлом и делает поведение предсказуемым.
	db.SetMaxOpenConns(1)

	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("can't connect to database: %w", err)
	}

	return &Storage{db: db}, nil
}

// Close закрывает пул соединений.
func (s *Storage) Close() error {
	return s.db.Close()
}

// Save сохраняет показания. Уникальный индекс (date, day_part, user_id) плюс
// ON CONFLICT DO NOTHING устраняют гонку (TOCTOU): признак вставки берётся из
// RowsAffected. false — запись за эту часть суток уже существует.
func (s *Storage) Save(ctx context.Context, p *storage.Pressure) (bool, error) {
	q := `INSERT INTO blood_pressure (date, day_part, systolic, diastolic, heart_rate, user_id, user_name)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(date, day_part, user_id) DO NOTHING`

	res, err := s.db.ExecContext(ctx, q, p.Date, p.DayPart, p.Systolic, p.Diastolic, p.HeartRate, p.UserID, p.UserName)
	if err != nil {
		return false, fmt.Errorf("can't save pressures: %w", err)
	}

	affected, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("can't save pressures: %w", err)
	}

	return affected > 0, nil
}

// Show возвращает сегодняшние показания пользователя по user_id.
func (s *Storage) Show(ctx context.Context, userID int64) ([]storage.Pressure, error) {
	now := timeloc.Now()

	q := `SELECT date, day_part, systolic, diastolic, heart_rate, user_id, user_name FROM blood_pressure WHERE user_id = ? AND date = ?`
	rows, err := s.db.QueryContext(ctx, q, userID, now.Format(timeloc.DateFormat))
	if err != nil {
		return nil, fmt.Errorf("can't show Pressure: %w", err)
	}
	defer rows.Close()

	var pressures []storage.Pressure
	for rows.Next() {
		var p storage.Pressure
		err := rows.Scan(&p.Date, &p.DayPart, &p.Systolic, &p.Diastolic, &p.HeartRate, &p.UserID, &p.UserName)
		if err != nil {
			return nil, fmt.Errorf("can't show Pressure: %w", err)
		}
		pressures = append(pressures, p)
	}
	// Проверка ошибок после завершения цикла
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("can't show Pressure: %w", err)
	}

	return pressures, nil
}

// GetAll возвращает все показания пользователя за всё время. Дата в формате
// 2006-01-02 сортируется лексикографически, то есть хронологически.
func (s *Storage) GetAll(ctx context.Context, userID int64) ([]storage.Pressure, error) {
	q := `SELECT date, day_part, systolic, diastolic, heart_rate, user_id, user_name FROM blood_pressure WHERE user_id = ? ORDER BY date`
	rows, err := s.db.QueryContext(ctx, q, userID)
	if err != nil {
		return nil, fmt.Errorf("can't get all pressures: %w", err)
	}
	defer rows.Close()

	var pressures []storage.Pressure
	for rows.Next() {
		var p storage.Pressure
		err := rows.Scan(&p.Date, &p.DayPart, &p.Systolic, &p.Diastolic, &p.HeartRate, &p.UserID, &p.UserName)
		if err != nil {
			return nil, fmt.Errorf("can't get all pressures: %w", err)
		}
		pressures = append(pressures, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("can't get all pressures: %w", err)
	}

	return pressures, nil
}

// ClaimLegacy привязывает старые записи (user_id IS NULL) к userID по user_name.
// OR IGNORE защищает от конфликта с уникальным индексом: неперенесённые
// остатки остаются невидимыми, но запись не ломают.
func (s *Storage) ClaimLegacy(ctx context.Context, userID int64, userName string) error {
	q := `UPDATE OR IGNORE blood_pressure SET user_id = ? WHERE user_id IS NULL AND user_name = ?`

	if _, err := s.db.ExecContext(ctx, q, userID, userName); err != nil {
		return fmt.Errorf("can't claim legacy pressures: %w", err)
	}

	return nil
}

// Init приводит схему БД к последней версии через механизм миграций.
func (s *Storage) Init(ctx context.Context) error {
	return s.migrate(ctx)
}
