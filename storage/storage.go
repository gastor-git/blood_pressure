package storage

import (
	"context"
	"errors"
)

type Storage interface {
	Save(ctx context.Context, p *Pressure) error
	Show(ctx context.Context, userName string) (string, error)
	Remove(ctx context.Context, p *Pressure) error
	IsExists(ctx context.Context, p *Pressure) (bool, error)
}

var ErrNoSavedPages = errors.New("no saved pages")
var ErrNoSavedPressure = errors.New("нет показаний за сегодня")

type Page struct {
	Url      string
	UserName string
}

type Pressure struct {
	Date      string
	DayPart   string
	Systolic  string
	Diastolic string
	HeartRate string
	UserName  string
}
