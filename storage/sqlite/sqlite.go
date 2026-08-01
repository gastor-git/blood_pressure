package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

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

// Save saves Pressure to storage.
func (s *Storage) Save(ctx context.Context, p *storage.Pressure) error {
	q := `INSERT INTO blood_pressure (date, day_part, systolic, diastolic, heart_rate, user_name) VALUES (?, ?, ?, ?, ?, ?)`

	if _, err := s.db.ExecContext(ctx, q, p.Date, p.DayPart, p.Systolic, p.Diastolic, p.HeartRate, p.UserName); err != nil {
		return fmt.Errorf("can't save pressures: %w", err)
	}

	return nil
}

// Show today pressures.
func (s *Storage) Show(ctx context.Context, userName string) (string, error) {
	now := timeloc.Now()

	q := `SELECT date, day_part, systolic, diastolic, heart_rate, user_name FROM blood_pressure WHERE user_name = ? AND date = ?`
	rows, err := s.db.QueryContext(ctx, q, userName, now.Format(timeloc.DateFormat))
	if err != nil {
		return "", fmt.Errorf("can't show Pressure: %w", err)
	}
	defer rows.Close()

	var pressures []storage.Pressure
	for rows.Next() {
		var p storage.Pressure
		err := rows.Scan(&p.Date, &p.DayPart, &p.Systolic, &p.Diastolic, &p.HeartRate, &p.UserName)
		if err != nil {
			return "", fmt.Errorf("can't show Pressure: %w", err)
		}
		pressures = append(pressures, p)
	}
	// Проверка ошибок после завершения цикла
	if err := rows.Err(); err != nil {
		return "", fmt.Errorf("can't show Pressure: %w", err)
	}

	var b strings.Builder
	for _, pressure := range pressures {
		b.WriteString("Дата: ")
		b.WriteString(pressure.Date)
		b.WriteString(", часть суток: ")
		b.WriteString(pressure.DayPart)
		b.WriteString(", показания: ")
		b.WriteString(pressure.Systolic)
		b.WriteString("/")
		b.WriteString(pressure.Diastolic)
		b.WriteString("/")
		b.WriteString(pressure.HeartRate)
		b.WriteString("\n\n")
	}

	return b.String(), nil
}

// IsExists checks if Pressure exists in storage.
func (s *Storage) IsExists(ctx context.Context, Pressure *storage.Pressure) (bool, error) {
	q := `SELECT COUNT(*) FROM blood_pressure WHERE date = ? AND day_part = ? AND user_name = ?`

	var count int

	if err := s.db.QueryRowContext(ctx, q, Pressure.Date, Pressure.DayPart, Pressure.UserName).Scan(&count); err != nil {
		return false, fmt.Errorf("can't check if pressure exists: %w", err)
	}

	return count > 0, nil
}

func (s *Storage) Init(ctx context.Context) error {
	q := `CREATE TABLE IF NOT EXISTS blood_pressure (date TEXT, day_part TEXT, systolic TEXT, diastolic TEXT, heart_rate TEXT, user_name TEXT)`

	_, err := s.db.ExecContext(ctx, q)
	if err != nil {
		return fmt.Errorf("can't create table: %w", err)
	}

	return nil
}
