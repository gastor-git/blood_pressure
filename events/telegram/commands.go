package telegram

import (
	"context"
	"errors"
	"log"
	"regexp"
	"strings"
	"time"

	"blood-pressure-bot/lib/e"
	"blood-pressure-bot/lib/timeloc"
	"blood-pressure-bot/storage"
)

const (
	ShowCmd  = "/show"
	HelpCmd  = "/help"
	StartCmd = "/start"
)

var (
	pressureRe = regexp.MustCompile(`^\d{2,3} \d{2,3} \d{2,3}$`)
	numberRe   = regexp.MustCompile(`\d+`)
)

// ErrInvalidPressure — защита от рассинхронизации isPressure и getPressures.
var ErrInvalidPressure = errors.New("неверный формат показаний давления")

func (p *Processor) doCmd(ctx context.Context, text string, chatID int, username string) error {
	text = strings.TrimSpace(text)

	log.Printf("got new command '%s' from '%s'", text, username)

	if isPressure(text) {
		return p.savePressure(ctx, chatID, text, username)
	}

	switch text {
	case ShowCmd:
		return p.show(ctx, chatID, username)
	case HelpCmd:
		return p.sendHelp(ctx, chatID)
	case StartCmd:
		return p.sendHello(ctx, chatID)
	default:
		return p.tg.SendMessage(ctx, chatID, msgUnknownCommand)
	}
}

func (p *Processor) savePressure(ctx context.Context, chatID int, text string, username string) (err error) {
	defer func() {
		err = e.WrapIfErr("Ошибка при сохранении показаний давления", err)
	}()

	now := timeloc.Now()

	datePart := dayPart(now)

	pressures := getPressures(text)
	if len(pressures) < 3 {
		return ErrInvalidPressure
	}

	pressure := &storage.Pressure{
		Date:      now.Format(timeloc.DateFormat),
		DayPart:   datePart,
		Systolic:  pressures[0],
		Diastolic: pressures[1],
		HeartRate: pressures[2],
		UserName:  username,
	}

	isExists, err := p.storage.IsExists(ctx, pressure)
	if err != nil {
		_ = p.tg.SendMessage(ctx, chatID, msgError)
		return err
	}
	if isExists {
		return p.tg.SendMessage(ctx, chatID, msgAlreadyExists)
	}

	if err := p.storage.Save(ctx, pressure); err != nil {
		_ = p.tg.SendMessage(ctx, chatID, msgError)
		return err
	}

	if err := p.tg.SendMessage(ctx, chatID, msgSaved); err != nil {
		return err
	}

	return nil
}

func (p *Processor) show(ctx context.Context, chatID int, username string) (err error) {
	defer func() { err = e.WrapIfErr("Ошибка при выполнении команды: show", err) }()

	msg, err := p.storage.Show(ctx, username)
	if err != nil {
		_ = p.tg.SendMessage(ctx, chatID, msgError)
		return err
	}

	if msg == "" {
		return p.tg.SendMessage(ctx, chatID, msgNoSavedPressure)
	}

	return p.tg.SendMessage(ctx, chatID, msg)
}

func (p *Processor) sendHelp(ctx context.Context, chatID int) error {
	return p.tg.SendMessage(ctx, chatID, msgHelp)
}

func (p *Processor) sendHello(ctx context.Context, chatID int) error {
	return p.tg.SendMessage(ctx, chatID, msgHello)
}

func dayPart(t time.Time) string {
	hour := t.Hour()

	if hour > 0 && hour <= 12 {
		return "утро"
	} else if hour <= 18 {
		return "день"
	}

	return "вечер"
}

func isPressure(text string) bool {
	return pressureRe.MatchString(text)
}

func getPressures(text string) []string {
	return numberRe.FindAllString(text, -1)
}
