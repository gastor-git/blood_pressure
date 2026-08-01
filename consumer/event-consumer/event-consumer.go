package event_consumer

import (
	"context"
	"log"
	"time"

	"blood-pressure-bot/events"
)

// errRetryDelay — пауза перед повтором после ошибки Fetch, чтобы не устраивать busy-loop.
const errRetryDelay = 1 * time.Second

type Consumer struct {
	fetcher   events.Fetcher
	processor events.Processor
	batchSize int
}

func New(fetcher events.Fetcher, processor events.Processor, batchSize int) Consumer {
	return Consumer{
		fetcher:   fetcher,
		processor: processor,
		batchSize: batchSize,
	}
}

func (c *Consumer) Start(ctx context.Context) error {
	for {
		if ctx.Err() != nil {
			return nil
		}

		gotEvents, err := c.fetcher.Fetch(ctx, c.batchSize)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}

			log.Printf("[ERR] consumer: %s", err.Error())

			select {
			case <-ctx.Done():
				return nil
			case <-time.After(errRetryDelay):
			}

			continue
		}

		if len(gotEvents) == 0 {
			continue
		}

		c.handleEvents(ctx, gotEvents)
	}
}

func (c *Consumer) handleEvents(ctx context.Context, events []events.Event) {
	for _, event := range events {
		c.handleEvent(ctx, event)
	}
}

// handleEvent обрабатывает одно событие и гасит панику, чтобы один сбой
// не уронил сервис для всех пользователей.
func (c *Consumer) handleEvent(ctx context.Context, event events.Event) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[ERR] panic while processing event: %v", r)
		}
	}()

	if err := c.processor.Process(ctx, event); err != nil {
		log.Printf("can't handle event: %s", err.Error())
	}
}
