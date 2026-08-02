package telegram

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"blood-pressure-bot/lib/e"
	"blood-pressure-bot/lib/timeloc"
	"blood-pressure-bot/storage"
)

// dialogState — шаг пошагового ввода показаний (/add).
type dialogState int

const (
	stateIdle dialogState = iota
	stateDate
	stateDayPart
	statePressure
	stateConfirmOverwrite
)

// session — состояние активного диалога ввода показаний пользователя.
type session struct {
	state    dialogState
	date     string // формат хранения 2006-01-02
	dayPart  string
	userName string
	// pending* — отложенные значения показаний при подтверждении перезаписи.
	pendingSys string
	pendingDia string
	pendingHr  string
}

// Клавиатуры диалога: выбора даты, части суток и подтверждения перезаписи.
var (
	dateKeyboard      = [][]string{{msgTodayButton}}
	dayPartKeyboard   = [][]string{{msgMorningButton, msgDayButton, msgEveningButton}}
	overwriteKeyboard = [][]string{{msgOverwriteButton}, {msgKeepButton}}
)

// startAdd начинает диалог ввода показаний: шаг выбора даты.
func (p *Processor) startAdd(ctx context.Context, chatID int, userID int64, username string) error {
	p.sessions[userID] = &session{state: stateDate, userName: username}

	return p.sendDatePrompt(ctx, chatID)
}

// handleDialog обрабатывает очередное сообщение активного диалога.
func (p *Processor) handleDialog(ctx context.Context, chatID int, userID int64, text string) error {
	s := p.sessions[userID]

	switch s.state {
	case stateDate:
		return p.handleDate(ctx, chatID, s, text)
	case stateDayPart:
		return p.handleDayPart(ctx, chatID, s, text)
	case statePressure:
		return p.handlePressure(ctx, chatID, userID, s, text)
	case stateConfirmOverwrite:
		return p.handleOverwrite(ctx, chatID, userID, s, text)
	default:
		return nil
	}
}

func (p *Processor) sendDatePrompt(ctx context.Context, chatID int) error {
	today := timeloc.Now().Format(timeloc.UserDateFormat)

	return p.tg.SendKeyboard(ctx, chatID, fmt.Sprintf(msgPromptDate, today), dateKeyboard, true)
}

func (p *Processor) sendDayPartPrompt(ctx context.Context, chatID int) error {
	return p.tg.SendKeyboard(ctx, chatID, fmt.Sprintf(msgPromptDayPart, DayPart(timeloc.Now())), dayPartKeyboard, true)
}

func (p *Processor) handleDate(ctx context.Context, chatID int, s *session, text string) error {
	if strings.EqualFold(text, msgTodayButton) {
		s.date = timeloc.Today()
		s.state = stateDayPart

		return p.sendDayPartPrompt(ctx, chatID)
	}

	date, err := parseUserDate(text)
	if err != nil {
		return p.tg.SendMessage(ctx, chatID, msgInvalidDate)
	}

	if date > timeloc.Today() {
		return p.tg.SendMessage(ctx, chatID, msgFutureDate)
	}

	s.date = date
	s.state = stateDayPart

	return p.sendDayPartPrompt(ctx, chatID)
}

// parseUserDate разбирает дату из формата ДД.ММ.ГГГГ и возвращает в формате
// хранения 2006-01-02.
func parseUserDate(text string) (string, error) {
	t, err := time.Parse(timeloc.UserDateFormat, text)
	if err != nil {
		return "", err
	}

	return t.Format(timeloc.DateFormat), nil
}

func (p *Processor) handleDayPart(ctx context.Context, chatID int, s *session, text string) error {
	part, ok := dayPartKey(text)
	if !ok {
		return p.tg.SendMessage(ctx, chatID, msgInvalidDayPart)
	}

	s.dayPart = part
	s.state = statePressure

	return p.tg.SendMessage(ctx, chatID, msgPromptPressure)
}

// dayPartKey сопоставляет текст кнопки/ввода части суток с ключом хранения.
func dayPartKey(text string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(text)) {
	case dayPartMorning:
		return dayPartMorning, true
	case dayPartDay:
		return dayPartDay, true
	case dayPartEvening:
		return dayPartEvening, true
	default:
		return "", false
	}
}

// handlePressure обрабатывает ввод всех показаний одним сообщением вида «120 80 70».
func (p *Processor) handlePressure(ctx context.Context, chatID int, userID int64, s *session, text string) error {
	if !isPressure(text) {
		return p.tg.SendMessage(ctx, chatID, msgInvalidPressureFormat)
	}

	sys, dia, hr, err := parsePressure(text)
	if err != nil {
		return p.tg.SendMessage(ctx, chatID, msgInvalidPressure)
	}

	return p.completeDialog(ctx, chatID, userID, s, sys, dia, hr)
}

func (p *Processor) completeDialog(ctx context.Context, chatID int, userID int64, s *session, sys, dia, hr string) (err error) {
	defer func() {
		err = e.WrapIfErr("Ошибка при сохранении показаний давления", err)
	}()

	res, err := p.save(ctx, userID, s.userName, s.date, s.dayPart, sys, dia, hr)
	if err != nil {
		p.cancelSession(userID)
		_ = p.tg.SendMessage(ctx, chatID, msgError)

		return err
	}

	if res == saveDuplicate {
		// Сессию не отменяем: переходим к подтверждению перезаписи.
		return p.confirmOverwrite(ctx, chatID, userID, s.date, s.dayPart, s.userName, sys, dia, hr)
	}

	p.cancelSession(userID)

	return p.tg.RemoveKeyboard(ctx, chatID, msgSaved)
}

// handleOverwrite обрабатывает ответ на вопрос о перезаписи дубликата.
func (p *Processor) handleOverwrite(ctx context.Context, chatID int, userID int64, s *session, text string) error {
	switch strings.TrimSpace(text) {
	case msgOverwriteButton:
		return p.doOverwrite(ctx, chatID, userID, s)
	case msgKeepButton:
		p.cancelSession(userID)

		return p.tg.RemoveKeyboard(ctx, chatID, msgKeepExisting)
	default:
		return p.tg.SendKeyboard(ctx, chatID, msgInvalidOverwriteChoice, overwriteKeyboard, true)
	}
}

// doOverwrite перезаписывает существующую запись отложенными значениями.
func (p *Processor) doOverwrite(ctx context.Context, chatID int, userID int64, s *session) (err error) {
	defer func() {
		err = e.WrapIfErr("Ошибка при сохранении показаний давления", err)
	}()

	// Дефензивная валидация отложенных значений: в штатном флоу они уже
	// прошли validatePressure при вводе.
	sys, err := strconv.Atoi(s.pendingSys)
	if err != nil {
		p.cancelSession(userID)

		return p.tg.SendMessage(ctx, chatID, msgInvalidPressure)
	}
	dia, err := strconv.Atoi(s.pendingDia)
	if err != nil {
		p.cancelSession(userID)

		return p.tg.SendMessage(ctx, chatID, msgInvalidPressure)
	}
	hr, err := strconv.Atoi(s.pendingHr)
	if err != nil {
		p.cancelSession(userID)

		return p.tg.SendMessage(ctx, chatID, msgInvalidPressure)
	}
	if err := validatePressure(sys, dia, hr); err != nil {
		p.cancelSession(userID)

		return p.tg.SendMessage(ctx, chatID, msgInvalidPressure)
	}

	if err := p.storage.Update(ctx, &storage.Pressure{
		Date:      s.date,
		DayPart:   s.dayPart,
		Systolic:  s.pendingSys,
		Diastolic: s.pendingDia,
		HeartRate: s.pendingHr,
		UserID:    userID,
		UserName:  s.userName,
	}); err != nil {
		p.cancelSession(userID)
		_ = p.tg.SendMessage(ctx, chatID, msgError)

		return err
	}

	p.cancelSession(userID)

	return p.tg.RemoveKeyboard(ctx, chatID, msgOverwritten)
}

// cancelSession завершает активный диалог пользователя.
func (p *Processor) cancelSession(userID int64) {
	delete(p.sessions, userID)
}
