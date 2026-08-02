package cli

import (
	"context"
	"flag"
	"fmt"
	"io"

	"blood-pressure-bot/lib/timeloc"
)

// defaultBackupName возвращает имя файла бэкапа по умолчанию.
func defaultBackupName() string {
	return "backup_" + timeloc.Now().Format(timeloc.DateFormat) + ".db"
}

// runBackup выполняет подкоманду backup: создаёт консистентную копию БД.
func runBackup(args []string, s store) error {
	fs := flag.NewFlagSet("backup", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	out := fs.String("out", defaultBackupName(), "путь к файлу бэкапа")

	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("не удалось разобрать флаги backup: %w", err)
	}

	if err := s.Init(context.Background()); err != nil {
		return fmt.Errorf("не удалось инициализировать БД: %w", err)
	}

	if err := s.BackupTo(context.Background(), *out); err != nil {
		return fmt.Errorf("не удалось создать бэкап: %w", err)
	}

	fmt.Printf("Бэкап создан: %s\n", *out)

	return nil
}
