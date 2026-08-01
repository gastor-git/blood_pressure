package telegram

import (
	"context"
	"errors"
	"fmt"
	"log"
	"regexp"
	"strconv"
	"strings"
	"time"

	"blood-pressure-bot/lib/e"
	"blood-pressure-bot/lib/timeloc"
	"blood-pressure-bot/storage"
)

const (
	ShowCmd     = "/show"
	HelpCmd     = "/help"
	StartCmd    = "/start"
	DownloadCmd = "/download"
)

const (
	dayPartMorning = "утро"
	dayPartDay     = "день"
	dayPartEvening = "вечер"
)

var (
	pressureRe = regexp.MustCompile(`^\d{2,3} \d{2,3} \d{2,3}$`)
	numberRe   = regexp.MustCompile(`\d+`)
)

// ErrInvalidPressure — защита от рассинхронизации isPressure и getPressures.
var ErrInvalidPressure = errors.New("неверный формат показаний давления")

// Границы валидации показаний (мягкие медицинские пределы).
const (
	systolicMin  = 60
	systolicMax  = 260
	diastolicMin = 30
	diastolicMax = 200
	heartRateMin = 30
	heartRateMax = 220
)

// Sentinel-ошибки валидации диапазонов. Это пользовательский ввод, а не сбой сервиса.
var (
	ErrSystolicOutOfRange  = errors.New("систолическое давление вне допустимого диапазона")
	ErrDiastolicOutOfRange = errors.New("диастолическое давление вне допустимого диапазона")
	ErrHeartRateOutOfRange = errors.New("пульс вне допустимого диапазона")
	ErrSystolicNotGreater  = errors.New("систолическое давление должно быть больше диастолического")
)

// validatePressure проверяет медицинские диапазоны показаний.
func validatePressure(sys, dia, hr int) error {
	if sys < systolicMin || sys > systolicMax {
		return ErrSystolicOutOfRange
	}
	if dia < diastolicMin || dia > diastolicMax {
		return ErrDiastolicOutOfRange
	}
	if hr < heartRateMin || hr > heartRateMax {
		return ErrHeartRateOutOfRange
	}
	if sys <= dia {
		return ErrSystolicNotGreater
	}

	return nil
}

func (p *Processor) doCmd(ctx context.Context, text string, chatID int, userID int64, username string) error {
	text = strings.TrimSpace(text)

	log.Printf("got new command '%s' from user %d", text, userID)

	if err := p.claimLegacy(ctx, userID, username); err != nil {
		return err
	}

	if isPressure(text) {
		return p.savePressure(ctx, chatID, text, userID, username)
	}

	switch text {
	case ShowCmd:
		return p.show(ctx, chatID, userID)
	case DownloadCmd:
		return p.download(ctx, chatID, userID, username)
	case HelpCmd:
		return p.sendHelp(ctx, chatID)
	case StartCmd:
		return p.sendHello(ctx, chatID)
	default:
		return p.tg.SendMessage(ctx, chatID, msgUnknownCommand)
	}
}

// claimLegacy один раз за время жизни процесса переносит старые записи
// пользователя на его user_id. Вызывается только при непустом username.
func (p *Processor) claimLegacy(ctx context.Context, userID int64, username string) error {
	if username == "" || p.claimed[userID] {
		return nil
	}

	if err := p.storage.ClaimLegacy(ctx, userID, username); err != nil {
		return e.Wrap("не удалось перенести старые показания", err)
	}

	p.claimed[userID] = true

	return nil
}

func (p *Processor) savePressure(ctx context.Context, chatID int, text string, userID int64, username string) (err error) {
	defer func() {
		err = e.WrapIfErr("Ошибка при сохранении показаний давления", err)
	}()

	now := timeloc.Now()

	datePart := dayPart(now)

	pressures := getPressures(text)
	if len(pressures) < 3 {
		return ErrInvalidPressure
	}

	sys, err := strconv.Atoi(pressures[0])
	if err != nil {
		return ErrInvalidPressure
	}
	dia, err := strconv.Atoi(pressures[1])
	if err != nil {
		return ErrInvalidPressure
	}
	hr, err := strconv.Atoi(pressures[2])
	if err != nil {
		return ErrInvalidPressure
	}

	if err := validatePressure(sys, dia, hr); err != nil {
		// Пользовательский ввод вне диапазона — не сбой сервиса.
		return p.tg.SendMessage(ctx, chatID, msgInvalidPressure)
	}

	pressure := &storage.Pressure{
		Date:      now.Format(timeloc.DateFormat),
		DayPart:   datePart,
		Systolic:  pressures[0],
		Diastolic: pressures[1],
		HeartRate: pressures[2],
		UserID:    userID,
		UserName:  username,
	}

	saved, err := p.storage.Save(ctx, pressure)
	if err != nil {
		_ = p.tg.SendMessage(ctx, chatID, msgError)
		return err
	}
	if !saved {
		// Гонки нет: уникальный индекс + ON CONFLICT DO NOTHING.
		return p.tg.SendMessage(ctx, chatID, msgAlreadyExists)
	}

	if err := p.tg.SendMessage(ctx, chatID, msgSaved); err != nil {
		return err
	}

	return nil
}

func (p *Processor) show(ctx context.Context, chatID int, userID int64) (err error) {
	defer func() { err = e.WrapIfErr("Ошибка при выполнении команды: show", err) }()

	res, err := p.storage.Show(ctx, userID)
	if err != nil {
		_ = p.tg.SendMessage(ctx, chatID, msgError)
		return err
	}

	if len(res) == 0 {
		return p.tg.SendMessage(ctx, chatID, msgNoSavedPressure)
	}

	return p.tg.SendMessage(ctx, chatID, formatPressures(res))
}

// download формирует CSV-файл с показаниями за всё время и отправляет его.
func (p *Processor) download(ctx context.Context, chatID int, userID int64, username string) (err error) {
	defer func() { err = e.WrapIfErr("Ошибка при выполнении команды: download", err) }()

	pressures, err := p.storage.GetAll(ctx, userID)
	if err != nil {
		_ = p.tg.SendMessage(ctx, chatID, msgError)
		return err
	}

	if len(pressures) == 0 {
		return p.tg.SendMessage(ctx, chatID, msgNoSavedPressure)
	}

	return p.tg.SendDocument(ctx, chatID, csvFilename(username), []byte(formatCSV(pressures)))
}

// formatPressures собирает пользовательский текст из показаний.
func formatPressures(pressures []storage.Pressure) string {
	var b strings.Builder
	for _, p := range pressures {
		fmt.Fprintf(&b, msgPressureLine, p.Date, p.DayPart, p.Systolic, p.Diastolic, p.HeartRate)
	}

	return b.String()
}

func (p *Processor) sendHelp(ctx context.Context, chatID int) error {
	return p.tg.SendMessage(ctx, chatID, msgHelp)
}

func (p *Processor) sendHello(ctx context.Context, chatID int) error {
	return p.tg.SendMessage(ctx, chatID, msgHello)
}

func dayPart(t time.Time) string {
	hour := t.Hour()

	switch {
	case hour < 12:
		return dayPartMorning
	case hour < 18:
		return dayPartDay
	default:
		return dayPartEvening
	}
}

func isPressure(text string) bool {
	return pressureRe.MatchString(text)
}

func getPressures(text string) []string {
	return numberRe.FindAllString(text, -1)
}
