package cli

import (
	"context"
)

// runHealth выполняет подкоманду health: проверяет доступность БД.
// Используется как healthcheck контейнера (без TG_KEY).
func runHealth(_ []string, s store) error {
	return s.Init(context.Background())
}
