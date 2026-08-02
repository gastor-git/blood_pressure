package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
)

// runDelete выполняет подкоманду delete. Без флага --yes команда отказывает;
// пустой фильтр удаляет все записи.
func runDelete(args []string, s store) error {
	fs := flag.NewFlagSet("delete", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	ff := addFilterFlags(fs)
	yes := fs.Bool("yes", false, "подтверждение удаления")

	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("не удалось разобрать флаги delete: %w", err)
	}

	if !*yes {
		return fmt.Errorf("удаление требует подтверждения: укажите флаг --yes. Без фильтров команда удалит ВСЕ записи")
	}

	filter, err := ff.filter()
	if err != nil {
		return err
	}

	n, err := s.Delete(context.Background(), filter)
	if err != nil {
		return fmt.Errorf("не удалось удалить показания: %w", err)
	}

	fmt.Printf("Удалено %d записей\n", n)

	return nil
}
