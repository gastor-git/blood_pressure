package telegram

import (
	"context"
	"errors"
	"log"
	"regexp"
	"strings"
	"time"

	"blood-pressure-bot/lib/e"
	"blood-pressure-bot/storage"
)

const (
	ShowCmd  = "/show"
	HelpCmd  = "/help"
	StartCmd = "/start"
)

func (p *Processor) doCmd(text string, chatID int, username string) error {
	text = strings.TrimSpace(text)

	log.Printf("got new command '%s' from '%s", text, username)

	if isPressure(text) {
		return p.savePressure(chatID, text, username)
	}

	switch text {
	case ShowCmd:
		return p.show(chatID, username)
	case HelpCmd:
		return p.sendHelp(chatID)
	case StartCmd:
		return p.sendHello(chatID)
	default:
		return p.tg.SendMessage(chatID, msgUnknownCommand)
	}
}

func (p *Processor) savePressure(chatID int, text string, username string) (err error) {
	defer func() {
		err = e.WrapIfErr("Ошибка при сохранении показаний давления", err)
	}()

	loc, err := time.LoadLocation("Asia/Yekaterinburg")
	if err != nil {
		return err
	}
	now := time.Now().In(loc)

	hour := now.Hour()
	var datePart string

	if hour > 0 && hour <= 12 {
		datePart = "утро"
	} else if hour <= 18 {
		datePart = "день"
	} else {
		datePart = "вечер"
	}

	pressures := getPressures(text)

	pressure := &storage.Pressure{
		Date:      now.Format("2006-01-02"),
		DayPart:   datePart,
		Systolic:  pressures[0],
		Diastolic: pressures[1],
		HeartRate: pressures[2],
		UserName:  username,
	}

	isExists, err := p.storage.IsExists(context.Background(), pressure)
	if err != nil {
		return err
	}
	if isExists {
		return p.tg.SendMessage(chatID, msgAlreadyExists)
	}

	if err := p.storage.Save(context.Background(), pressure); err != nil {
		return err
	}

	if err := p.tg.SendMessage(chatID, msgSaved); err != nil {
		return err
	}

	return nil
}

func (p *Processor) show(chatID int, username string) (err error) {
	defer func() { err = e.WrapIfErr("Ошибка при выполнении команды: show", err) }()

	msg, err := p.storage.Show(context.Background(), username)

	if err != nil && !errors.Is(err, storage.ErrNoSavedPressure) {
		return err
	}

	if errors.Is(err, storage.ErrNoSavedPressure) || msg == "" {
		return p.tg.SendMessage(chatID, msgNoSavedPressure)
	}

	if err := p.tg.SendMessage(chatID, msg); err != nil {
		return err
	}

	return nil
}

func (p *Processor) sendHelp(chatID int) error {
	return p.tg.SendMessage(chatID, msgHelp)
}

func (p *Processor) sendHello(chatID int) error {
	return p.tg.SendMessage(chatID, msgHello)
}

func isPressure(text string) bool {
	matched, err := regexp.MatchString(`^\d{2,3} \d{2,3} \d{2,3}$`, text)

	return err == nil && matched
}

func getPressures(text string) []string {
	re := regexp.MustCompile(`\d+`)

	// Найти все совпадения
	matches := re.FindAllString(text, -1)

	return matches
}
