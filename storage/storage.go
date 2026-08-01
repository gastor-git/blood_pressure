package storage

import "context"

type Storage interface {
	Save(ctx context.Context, p *Pressure) error
	Show(ctx context.Context, userName string) (string, error)
	IsExists(ctx context.Context, p *Pressure) (bool, error)
}

type Pressure struct {
	Date      string
	DayPart   string
	Systolic  string
	Diastolic string
	HeartRate string
	UserName  string
}
