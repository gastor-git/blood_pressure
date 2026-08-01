package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

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

func mustToken() string {
	token := os.Getenv("TG_KEY")
	if token == "" {
		log.Fatal("TG_KEY is not set")
	}

	return token
}
