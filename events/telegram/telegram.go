package telegram

import (
	"context"
	"errors"

	"blood-pressure-bot/clients/telegram"
	"blood-pressure-bot/events"
	"blood-pressure-bot/lib/e"
	"blood-pressure-bot/storage"
)

type Client interface {
	Updates(ctx context.Context, offset, limit int) ([]telegram.Update, error)
	SendMessage(ctx context.Context, chatID int, text string) error
	SendDocument(ctx context.Context, chatID int, filename string, data []byte) error
}

type Processor struct {
	tg      Client
	offset  int
	storage storage.Storage
	// claimed — пользователи, для которых ленивый backfill legacy-записей
	// уже выполнен за время жизни процесса. Консьюмер однопоточный, поэтому
	// мьютекс не нужен.
	claimed map[int64]bool
}

type Meta struct {
	ChatID   int
	UserID   int64
	Username string
}

var (
	ErrUnknownEventType = errors.New("unknown event type")
	ErrUnknownMetaType  = errors.New("unknown meta type")
)

func New(client Client, storage storage.Storage) *Processor {
	return &Processor{
		tg:      client,
		storage: storage,
		claimed: make(map[int64]bool),
	}
}

func (p *Processor) Fetch(ctx context.Context, limit int) ([]events.Event, error) {
	updates, err := p.tg.Updates(ctx, p.offset, limit)
	if err != nil {
		return nil, e.Wrap("can't get events", err)
	}

	if len(updates) == 0 {
		return nil, nil
	}

	res := make([]events.Event, 0, len(updates))

	for _, u := range updates {
		res = append(res, event(u))
	}

	p.offset = updates[len(updates)-1].ID + 1

	return res, nil
}

func (p *Processor) Process(ctx context.Context, event events.Event) error {
	switch event.Type {
	case events.Message:
		return p.processMessage(ctx, event)
	default:
		return e.Wrap("can't process message", ErrUnknownEventType)
	}
}

func (p *Processor) processMessage(ctx context.Context, event events.Event) error {
	meta, err := meta(event)
	if err != nil {
		return e.Wrap("can't process message", err)
	}

	if err := p.doCmd(ctx, event.Text, meta.ChatID, meta.UserID, meta.Username); err != nil {
		return e.Wrap("can't process message", err)
	}

	return nil
}

func meta(event events.Event) (Meta, error) {
	res, ok := event.Meta.(Meta)
	if !ok {
		return Meta{}, e.Wrap("can't get meta", ErrUnknownMetaType)
	}

	return res, nil
}

func event(upd telegram.Update) events.Event {
	updType := fetchType(upd)

	res := events.Event{
		Type: updType,
		Text: fetchText(upd),
	}

	if updType == events.Message {
		res.Meta = Meta{
			ChatID:   upd.Message.Chat.ID,
			UserID:   upd.Message.From.ID,
			Username: upd.Message.From.Username,
		}
	}

	return res
}

func fetchText(upd telegram.Update) string {
	if upd.Message == nil {
		return ""
	}

	return upd.Message.Text
}

func fetchType(upd telegram.Update) events.Type {
	if upd.Message == nil {
		return events.Unknown
	}

	return events.Message
}
