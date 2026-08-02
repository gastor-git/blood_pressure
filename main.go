package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"blood-pressure-bot/cli"
	tgClient "blood-pressure-bot/clients/telegram"
	event_consumer "blood-pressure-bot/consumer/event-consumer"
	"blood-pressure-bot/events/telegram"
	"blood-pressure-bot/notifier"
	"blood-pressure-bot/storage/sqlite"
)

const (
	tgBotHost         = "api.telegram.org"
	sqliteStoragePath = "data/sqlite/storage.db"
	batchSize         = 100
)

func main() {
	// CLI-режим: export | delete | help. Запускается без TG_KEY.
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "export", "delete", "help":
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

	log.Print("service started")

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
