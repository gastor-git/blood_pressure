package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

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

// Get возвращает запись показаний по ключу (user_id, date, day_part).
// (nil, nil) — записи нет.
func (s *Storage) Get(ctx context.Context, userID int64, date, dayPart string) (*storage.Pressure, error) {
	q := `SELECT date, day_part, systolic, diastolic, heart_rate, user_id, user_name FROM blood_pressure WHERE user_id = ? AND date = ? AND day_part = ?`

	var p storage.Pressure
	err := s.db.QueryRowContext(ctx, q, userID, date, dayPart).
		Scan(&p.Date, &p.DayPart, &p.Systolic, &p.Diastolic, &p.HeartRate, &p.UserID, &p.UserName)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("can't get pressure: %w", err)
	}

	return &p, nil
}

// Update перезаписывает показания существующей записи по ключу
// (user_id, date, day_part). Ошибка (sql.ErrNoRows) — записи нет.
func (s *Storage) Update(ctx context.Context, p *storage.Pressure) error {
	q := `UPDATE blood_pressure SET systolic = ?, diastolic = ?, heart_rate = ?, user_name = ? WHERE user_id = ? AND date = ? AND day_part = ?`

	res, err := s.db.ExecContext(ctx, q, p.Systolic, p.Diastolic, p.HeartRate, p.UserName, p.UserID, p.Date, p.DayPart)
	if err != nil {
		return fmt.Errorf("can't update pressures: %w", err)
	}

	affected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("can't update pressures: %w", err)
	}
	if affected == 0 {
		return fmt.Errorf("can't update pressures: %w", sql.ErrNoRows)
	}

	return nil
}

// Show возвращает показания пользователя за конкретную дату.
func (s *Storage) Show(ctx context.Context, userID int64, date string) ([]storage.Pressure, error) {
	q := `SELECT date, day_part, systolic, diastolic, heart_rate, user_id, user_name FROM blood_pressure WHERE user_id = ? AND date = ?`
	rows, err := s.db.QueryContext(ctx, q, userID, date)
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

// ExportAll возвращает все записи, удовлетворяющие фильтру, упорядоченные по
// user_id, date. Legacy-строки (user_id IS NULL) попадают в выборку: фильтр по
// user_name их находит, по user_id — нет.
func (s *Storage) ExportAll(ctx context.Context, filter storage.Filter) ([]storage.Pressure, error) {
	q := `SELECT date, day_part, systolic, diastolic, heart_rate, user_id, user_name FROM blood_pressure`
	where, args := buildFilterWhere(filter)
	if where != "" {
		q += " WHERE " + where
	}
	q += ` ORDER BY user_id, date`

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("can't export pressures: %w", err)
	}
	defer rows.Close()

	var pressures []storage.Pressure
	for rows.Next() {
		var p storage.Pressure
		// user_id / user_name у legacy-строк могут быть NULL: сканируем через
		// Null-типы и нормализуем до нулевых значений.
		var uid sql.NullInt64
		var uname sql.NullString
		if err := rows.Scan(&p.Date, &p.DayPart, &p.Systolic, &p.Diastolic, &p.HeartRate, &uid, &uname); err != nil {
			return nil, fmt.Errorf("can't export pressures: %w", err)
		}
		p.UserID = uid.Int64
		p.UserName = uname.String
		pressures = append(pressures, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("can't export pressures: %w", err)
	}

	return pressures, nil
}

// Delete удаляет записи, удовлетворяющие фильтру, и возвращает число удалённых.
// Пустой фильтр удаляет все записи.
func (s *Storage) Delete(ctx context.Context, filter storage.Filter) (int64, error) {
	q := `DELETE FROM blood_pressure`
	where, args := buildFilterWhere(filter)
	if where != "" {
		q += " WHERE " + where
	}

	res, err := s.db.ExecContext(ctx, q, args...)
	if err != nil {
		return 0, fmt.Errorf("can't delete pressures: %w", err)
	}

	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("can't delete pressures: %w", err)
	}

	return n, nil
}

// buildFilterWhere собирает WHERE-условие из фильтра. Значения только через
// параметры (?), конкатенация пользовательского ввода в SQL запрещена.
func buildFilterWhere(filter storage.Filter) (string, []any) {
	var conds []string
	var args []any

	if filter.UserID != nil {
		conds = append(conds, "user_id = ?")
		args = append(args, *filter.UserID)
	}
	if filter.UserName != nil {
		conds = append(conds, "user_name = ?")
		args = append(args, *filter.UserName)
	}
	if filter.From != nil {
		conds = append(conds, "date >= ?")
		args = append(args, *filter.From)
	}
	if filter.To != nil {
		conds = append(conds, "date <= ?")
		args = append(args, *filter.To)
	}

	return strings.Join(conds, " AND "), args
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

// RegisterUser upsert-ом сохраняет пользователя при каждом входящем сообщении.
// Если запись уже есть — обновляются chat_id, user_name и updated_at.
func (s *Storage) RegisterUser(ctx context.Context, userID int64, chatID int64, userName string) error {
	q := `INSERT INTO users (user_id, chat_id, user_name, updated_at) VALUES (?, ?, ?, ?)
		ON CONFLICT(user_id) DO UPDATE SET chat_id = excluded.chat_id, user_name = excluded.user_name, updated_at = excluded.updated_at`

	if _, err := s.db.ExecContext(ctx, q, userID, chatID, userName, timeloc.Now().Format(time.RFC3339)); err != nil {
		return fmt.Errorf("can't register user: %w", err)
	}

	return nil
}

// AllUsers возвращает всех зарегистрированных пользователей с их utc_offset.
// Отбор получателей напоминаний (по дате/части суток в персональной таймзоне)
// выполняется в notifier.
func (s *Storage) AllUsers(ctx context.Context) ([]storage.User, error) {
	q := `SELECT user_id, chat_id, user_name, utc_offset FROM users ORDER BY user_id`

	rows, err := s.db.QueryContext(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("can't get users: %w", err)
	}
	defer rows.Close()

	var users []storage.User
	for rows.Next() {
		var u storage.User
		if err := rows.Scan(&u.UserID, &u.ChatID, &u.UserName, &u.UTCOffset); err != nil {
			return nil, fmt.Errorf("can't get users: %w", err)
		}
		users = append(users, u)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("can't get users: %w", err)
	}

	return users, nil
}

// SetUTCOffset сохраняет utc_offset (секунды от UTC) для пользователя.
func (s *Storage) SetUTCOffset(ctx context.Context, userID int64, offset int) error {
	q := `UPDATE users SET utc_offset = ? WHERE user_id = ?`

	if _, err := s.db.ExecContext(ctx, q, offset, userID); err != nil {
		return fmt.Errorf("can't set utc offset: %w", err)
	}

	return nil
}

// Init приводит схему БД к последней версии через механизм миграций.
func (s *Storage) Init(ctx context.Context) error {
	return s.migrate(ctx)
}
