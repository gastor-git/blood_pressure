// Package timeloc хранит единую таймзону и формат даты учёта показаний.
// Локация загружается один раз при инициализации пакета, а не на каждый вызов.
package timeloc

import "time"

const (
	// TimeZone — таймзона, в которой определяются дата и часть суток.
	TimeZone = "Asia/Yekaterinburg"
	// DateFormat — формат хранения и сравнения даты.
	DateFormat = "2006-01-02"
)

// location загружается один раз при старте процесса.
var location = mustLoad()

func mustLoad() *time.Location {
	loc, err := time.LoadLocation(TimeZone)
	if err != nil {
		panic("timeloc: не удалось загрузить таймзону " + TimeZone + ": " + err.Error())
	}

	return loc
}

// Location возвращает предзагруженную таймзону учёта.
func Location() *time.Location {
	return location
}

// Now возвращает текущее время в таймзоне учёта.
func Now() time.Time {
	return time.Now().In(location)
}

// Today возвращает текущую дату в формате хранения.
func Today() string {
	return Now().Format(DateFormat)
}
