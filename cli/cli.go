// Package cli — командная строка для управления БД показаний давления.
package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"time"

	"blood-pressure-bot/lib/timeloc"
	"blood-pressure-bot/storage"
)

// store — интерфейс хранилища, потребляемый CLI. Объявлен на стороне
// потребителя (как принято в проекте); *storage/sqlite.Storage удовлетворяет
// ему неявно.
type store interface {
	Init(ctx context.Context) error
	ExportAll(ctx context.Context, filter storage.Filter) ([]storage.Pressure, error)
	Delete(ctx context.Context, filter storage.Filter) (int64, error)
	BackupTo(ctx context.Context, path string) error
	Close() error
}

// ErrUnknownCommand — вызов CLI без известной подкоманды.
var ErrUnknownCommand = errors.New("неизвестная команда")

// Run маршрутизирует подкоманды CLI: export | delete | backup | health | help.
// args — это os.Args[1:], то есть начинается с имени подкоманды.
func Run(args []string, s store) error {
	if len(args) == 0 {
		return fmt.Errorf("%w: не указана команда", ErrUnknownCommand)
	}

	switch args[0] {
	case "export":
		return runExport(args[1:], s)
	case "delete":
		return runDelete(args[1:], s)
	case "backup":
		return runBackup(args[1:], s)
	case "health":
		return runHealth(args[1:], s)
	case "help":
		runHelp()
		return nil
	default:
		return fmt.Errorf("%w %q, используйте: export | delete | backup | health | help", ErrUnknownCommand, args[0])
	}
}

// filterFlags — общие флаги фильтрации команд export и delete.
type filterFlags struct {
	userID   *int64
	userName *string
	from     *string
	to       *string
}

// addFilterFlags регистрирует флаги фильтрации в fs.
func addFilterFlags(fs *flag.FlagSet) *filterFlags {
	return &filterFlags{
		userID:   fs.Int64("user-id", 0, "фильтр по идентификатору пользователя"),
		userName: fs.String("user-name", "", "фильтр по имени пользователя (точное совпадение)"),
		from:     fs.String("from", "", "начало диапазона дат, ДД.ММ.ГГГГ (включительно)"),
		to:       fs.String("to", "", "конец диапазона дат, ДД.ММ.ГГГГ (включительно)"),
	}
}

// filter собирает storage.Filter из значений флагов. user-id = 0 означает
// «без фильтра» (вариант по умолчанию).
func (ff *filterFlags) filter() (storage.Filter, error) {
	return parseFilter(ff.userID, *ff.userName, *ff.from, *ff.to)
}

// parseFilter строит storage.Filter из значений флагов CLI. Даты из формата
// ДД.ММ.ГГГГ переводятся в формат хранения YYYY-MM-DD.
func parseFilter(userID *int64, userName, from, to string) (storage.Filter, error) {
	var f storage.Filter

	if userID != nil && *userID != 0 {
		f.UserID = userID
	}
	if userName != "" {
		f.UserName = &userName
	}
	if from != "" {
		d, err := parseCLIDate(from)
		if err != nil {
			return f, fmt.Errorf("неверное значение --from: %w", err)
		}
		f.From = &d
	}
	if to != "" {
		d, err := parseCLIDate(to)
		if err != nil {
			return f, fmt.Errorf("неверное значение --to: %w", err)
		}
		f.To = &d
	}

	if f.From != nil && f.To != nil && *f.From > *f.To {
		return f, errors.New("--from не может быть позже --to")
	}

	return f, nil
}

// parseCLIDate разбирает дату из формата ДД.ММ.ГГГГ и возвращает в формате
// хранения YYYY-MM-DD.
func parseCLIDate(text string) (string, error) {
	t, err := time.Parse(timeloc.UserDateFormat, text)
	if err != nil {
		return "", errors.New("ожидается ДД.ММ.ГГГГ")
	}

	return t.Format(timeloc.DateFormat), nil
}

// formatCSVDate приводит дату из формата хранения к виду ДД.ММ.ГГГГ.
func formatCSVDate(date string) string {
	t, err := time.Parse(timeloc.DateFormat, date)
	if err != nil {
		return date
	}

	return t.Format(timeloc.CSVDateFormat)
}
