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
	AddCmd      = "/add"
	CancelCmd   = "/cancel"
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

// validateSystolic проверяет диапазон систолического давления.
func validateSystolic(sys int) error {
	if sys < systolicMin || sys > systolicMax {
		return ErrSystolicOutOfRange
	}

	return nil
}

// validateDiastolic проверяет диапазон диастолического давления.
func validateDiastolic(dia int) error {
	if dia < diastolicMin || dia > diastolicMax {
		return ErrDiastolicOutOfRange
	}

	return nil
}

// validateHeartRate проверяет диапазон пульса.
func validateHeartRate(hr int) error {
	if hr < heartRateMin || hr > heartRateMax {
		return ErrHeartRateOutOfRange
	}

	return nil
}

// validatePressure проверяет медицинские диапазоны показаний.
func validatePressure(sys, dia, hr int) error {
	if err := validateSystolic(sys); err != nil {
		return err
	}
	if err := validateDiastolic(dia); err != nil {
		return err
	}
	if err := validateHeartRate(hr); err != nil {
		return err
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

	if err := p.storage.RegisterUser(ctx, userID, int64(chatID), username); err != nil {
		return e.Wrap("не удалось сохранить пользователя", err)
	}

	// Активный диалог /add: команда отменяет его, всё остальное — шаг диалога.
	if s, ok := p.sessions[userID]; ok && s.state != stateIdle {
		if strings.HasPrefix(text, "/") {
			p.cancelSession(userID)
		} else {
			return p.handleDialog(ctx, chatID, userID, text)
		}
	}

	if isPressure(text) {
		return p.savePressure(ctx, chatID, text, userID, username)
	}

	switch text {
	case AddCmd:
		return p.startAdd(ctx, chatID, userID, username)
	case CancelCmd:
		return p.tg.SendMessage(ctx, chatID, msgCancel)
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

	datePart := DayPart(now)

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

	res, err := p.save(ctx, userID, username, now.Format(timeloc.DateFormat), datePart, pressures[0], pressures[1], pressures[2])
	if err != nil {
		_ = p.tg.SendMessage(ctx, chatID, msgError)
		return err
	}

	switch res {
	case saveDuplicate:
		return p.tg.SendMessage(ctx, chatID, msgAlreadyExists)
	default:
		return p.tg.SendMessage(ctx, chatID, msgSaved)
	}
}

// saveResult — результат попытки сохранения показаний.
type saveResult int

const (
	saveSaved saveResult = iota
	saveDuplicate
	saveFailed
)

// save сохраняет показания с явными датой и частью суток. Ошибка хранилища
// возвращается как saveFailed + err; дубликат — saveDuplicate без ошибки.
func (p *Processor) save(ctx context.Context, userID int64, username, date, dayPart, sys, dia, hr string) (saveResult, error) {
	pressure := &storage.Pressure{
		Date:      date,
		DayPart:   dayPart,
		Systolic:  sys,
		Diastolic: dia,
		HeartRate: hr,
		UserID:    userID,
		UserName:  username,
	}

	saved, err := p.storage.Save(ctx, pressure)
	if err != nil {
		return saveFailed, err
	}
	if !saved {
		// Гонки нет: уникальный индекс + ON CONFLICT DO NOTHING.
		return saveDuplicate, nil
	}

	return saveSaved, nil
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

// DayPart возвращает метку части суток для времени t.
func DayPart(t time.Time) string {
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
