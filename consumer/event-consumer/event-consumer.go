package event_consumer

import (
	"context"
	"errors"
	"log"
	"time"

	"blood-pressure-bot/events"
)

// errRetryDelay — пауза перед повтором после ошибки Fetch, чтобы не устраивать
// busy-loop.
const errRetryDelay = 1 * time.Second

// maxRetryDelay — верхний предел паузы, рекомендованной Telegram (retry_after).
const maxRetryDelay = 60 * time.Second

// retryAfter — ошибка, которая сообщает рекомендованную паузу перед повтором
// (секунды). Реализуется *telegram.APIError; интерфейс объявлен на стороне
// потребителя, чтобы не тащить зависимость на конкретный клиент.
type retryAfter interface {
	RetryAfter() int
}

// retryDelay возвращает паузу перед повтором: retry_after из APIError (с
// верхним пределом) либо errRetryDelay.
func retryDelay(err error) time.Duration {
	var ra retryAfter
	if errors.As(err, &ra) && ra.RetryAfter() > 0 {
		d := time.Duration(ra.RetryAfter()) * time.Second
		if d > maxRetryDelay {
			return maxRetryDelay
		}
		return d
	}

	return errRetryDelay
}

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
			case <-time.After(retryDelay(err)):
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
