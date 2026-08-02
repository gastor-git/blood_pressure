package storage

import "context"

type Storage interface {
	// Save сохраняет показания. Возвращает false, если запись за эту часть
	// суток у пользователя уже есть (ON CONFLICT DO NOTHING).
	Save(ctx context.Context, p *Pressure) (bool, error)
	// Get возвращает запись по ключу (user_id, date, day_part).
	// (nil, nil) — записи нет.
	Get(ctx context.Context, userID int64, date, dayPart string) (*Pressure, error)
	// Update перезаписывает значения существующей записи по ключу
	// (user_id, date, day_part).
	Update(ctx context.Context, p *Pressure) error
	// Show возвращает показания пользователя за конкретную дату.
	Show(ctx context.Context, userID int64, date string) ([]Pressure, error)
	// GetAll возвращает все показания пользователя за всё время.
	GetAll(ctx context.Context, userID int64) ([]Pressure, error)
	// ClaimLegacy привязывает старые записи (user_id IS NULL) к userID по
	// user_name — ленивый backfill.
	ClaimLegacy(ctx context.Context, userID int64, userName string) error
	// RegisterUser — upsert пользователя при каждом входящем сообщении.
	RegisterUser(ctx context.Context, userID int64, chatID int64, userName string) error
	// SetUTCOffset сохраняет смещение таймзоны пользователя (секунды от UTC)
	// из getChat. Вызывается только при отличном от нуля offset.
	SetUTCOffset(ctx context.Context, userID int64, offset int) error
	// AllUsers возвращает всех зарегистрированных пользователей.
	AllUsers(ctx context.Context) ([]User, error)
}

type User struct {
	UserID    int64
	ChatID    int64
	UserName  string
	UTCOffset int
}

type Pressure struct {
	Date      string
	DayPart   string
	Systolic  string
	Diastolic string
	HeartRate string
	UserID    int64
	UserName  string
}
