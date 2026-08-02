package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"runtime/debug"
	"strconv"
	"syscall"

	"blood-pressure-bot/cli"
	tgClient "blood-pressure-bot/clients/telegram"
	event_consumer "blood-pressure-bot/consumer/event-consumer"
	"blood-pressure-bot/events/telegram"
	"blood-pressure-bot/notifier"
	"blood-pressure-bot/storage/sqlite"
)

const defaultBatchSize = 100

// Конфигурация через env с дефолтами; при необходимости переопределяется в
// .env / docker-compose.
var (
	tgBotHost         = envOr("BP_TG_HOST", "api.telegram.org")
	sqliteStoragePath = envOr("BP_DB_PATH", "data/sqlite/storage.db")
	batchSize         = envIntOr("BP_BATCH_SIZE", defaultBatchSize)
)

func main() {
	// CLI-режим: export | delete | backup | health | help. Запускается без TG_KEY.
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "export", "delete", "backup", "health", "help":
			os.Exit(runCLI())
		}
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	s, err := sqlite.New(sqliteStoragePath)
	if err != nil {
		log.Fatal("can't connect to storage: ", err)
	}
	defer func() { _ = s.Close() }()

	if err := s.Init(ctx); err != nil {
		log.Fatal("can't init storage: ", err)
	}

	eventsProcessor := telegram.New(
		tgClient.New(tgBotHost, mustToken()),
		s,
	)

	log.Printf("service started, version=%s, db=%s", buildVersion(), sqliteStoragePath)

	consumer := event_consumer.New(eventsProcessor, eventsProcessor, batchSize)

	go func() {
		if err := notifier.New(s, tgClient.New(tgBotHost, mustToken())).Start(ctx); err != nil {
			log.Print("notifier stopped: ", err)
		}
	}()

	if err := consumer.Start(ctx); err != nil {
		log.Fatal("service is stopped", err)
	}

	log.Print("service stopped")
}

// runCLI открывает БД, инициализирует схему и выполняет подкоманду CLI.
// Возвращает код выхода: 0 — успех, 1 — ошибка.
func runCLI() int {
	s, err := sqlite.New(sqliteStoragePath)
	if err != nil {
		log.Print("can't connect to storage: ", err)
		return 1
	}
	defer func() { _ = s.Close() }()

	if err := s.Init(context.Background()); err != nil {
		log.Print("can't init storage: ", err)
		return 1
	}

	if err := cli.Run(os.Args[1:], s); err != nil {
		log.Print(err)
		return 1
	}

	return 0
}

func mustToken() string {
	token := os.Getenv("TG_KEY")
	if token == "" {
		log.Fatal("TG_KEY is not set")
	}

	return token
}

// envOr возвращает значение переменной окружения key или def, если она пуста.
func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}

	return def
}

// envIntOr возвращает целочисленное значение переменной окружения key или def
// при пустом/невалидном значении.
func envIntOr(key string, def int) int {
	v := os.Getenv(key)
	if v == "" {
		return def
	}

	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		log.Printf("WARN: invalid %s=%q, using default %d", key, v, def)
		return def
	}

	return n
}

// buildVersion возвращает версию сборки из module build info ("(devel)" для
// локальных сборок, версия тега для опубликованных).
func buildVersion() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "unknown"
	}
	if info.Main.Version == "" {
		return "(devel)"
	}

	return info.Main.Version
}
