package storage

import "context"

type Storage interface {
	// Save сохраняет показания. Возвращает false, если запись за эту часть
	// суток у пользователя уже есть (ON CONFLICT DO NOTHING).
	Save(ctx context.Context, p *Pressure) (bool, error)
	Show(ctx context.Context, userID int64) ([]Pressure, error)
	// GetAll возвращает все показания пользователя за всё время.
	GetAll(ctx context.Context, userID int64) ([]Pressure, error)
	// ClaimLegacy привязывает старые записи (user_id IS NULL) к userID по
	// user_name — ленивый backfill.
	ClaimLegacy(ctx context.Context, userID int64, userName string) error
	// RegisterUser — upsert пользователя при каждом входящем сообщении.
	RegisterUser(ctx context.Context, userID int64, chatID int64, userName string) error
	// UsersWithoutPressure — пользователи без записи за дату+часть суток.
	UsersWithoutPressure(ctx context.Context, date, dayPart string) ([]User, error)
}

type User struct {
	UserID   int64
	ChatID   int64
	UserName string
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
