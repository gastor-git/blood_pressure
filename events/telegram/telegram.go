package telegram

import (
	"context"
	"errors"
	"log"

	"blood-pressure-bot/clients/telegram"
	"blood-pressure-bot/events"
	"blood-pressure-bot/lib/e"
	"blood-pressure-bot/storage"
)

type Client interface {
	Updates(ctx context.Context, offset, limit int) ([]telegram.Update, error)
	SendMessage(ctx context.Context, chatID int, text string) error
	SendDocument(ctx context.Context, chatID int, filename string, data []byte) error
	SendKeyboard(ctx context.Context, chatID int, text string, keyboard [][]string, oneTime bool) error
	RemoveKeyboard(ctx context.Context, chatID int, text string) error
	GetChat(ctx context.Context, chatID int) (*telegram.ChatFullInfo, error)
}

// offsetStore — персист offset getUpdates. Объявлен на стороне потребителя;
// *storage/sqlite.Storage удовлетворяет ему неявно. Опциональный: если
// хранилище его не реализует, offset остаётся в памяти (как раньше).
type offsetStore interface {
	GetOffset(ctx context.Context) (int, error)
	SetOffset(ctx context.Context, offset int) error
}

type Processor struct {
	tg      Client
	offset  int
	storage storage.Storage
	offsets offsetStore
	// offsetLoaded — персистенный offset уже прочитан (лениво в первом Fetch).
	offsetLoaded bool
	// claimed — пользователи, для которых ленивый backfill legacy-записей
	// уже выполнен за время жизни процесса. Консьюмер однопоточный, поэтому
	// мьютекс не нужен.
	claimed map[int64]bool
	// sessions — активные диалоги ввода показаний по user_id. Как и claimed,
	// живут в памяти и не требуют мьютекса: консьюмер однопоточный.
	sessions map[int64]*session
	// utcOffsets — закэшированные utc_offset пользователей (секунды от UTC)
	// из getChat; 0 = неизвестно. Как и claimed, без мьютекса.
	utcOffsets map[int64]int
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
	p := &Processor{
		tg:         client,
		storage:    storage,
		claimed:    make(map[int64]bool),
		sessions:   make(map[int64]*session),
		utcOffsets: make(map[int64]int),
	}

	// Персист offset опционален; загрузка — лениво в первом Fetch (нужен ctx).
	p.offsets, _ = storage.(offsetStore)

	return p
}

func (p *Processor) Fetch(ctx context.Context, limit int) ([]events.Event, error) {
	if !p.offsetLoaded && p.offsets != nil {
		if offset, err := p.offsets.GetOffset(ctx); err == nil {
			p.offset = offset
		}
		p.offsetLoaded = true
	}

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

	// Подтверждаем offset в БД, чтобы после рестарта Telegram не прислал
	// уже обработанные события. Сбой записи не прерывает цикл — повтор
	// записи произойдёт на следующей пачке.
	if p.offsets != nil {
		if err := p.offsets.SetOffset(ctx, p.offset); err != nil {
			log.Printf("[ERR] can't persist updates offset: %s", err.Error())
		}
	}

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
