package telegram

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"blood-pressure-bot/lib/e"
	"blood-pressure-bot/lib/timeloc"
)

// dialogState — шаг пошагового ввода показаний (/add).
type dialogState int

const (
	stateIdle dialogState = iota
	stateDate
	stateDayPart
	stateSystolic
	stateDiastolic
	stateHeartRate
)

// session — состояние активного диалога ввода показаний пользователя.
type session struct {
	state     dialogState
	date      string // формат хранения 2006-01-02
	dayPart   string
	systolic  string
	diastolic string
	// value — буфер набираемых с цифровой панели цифр.
	value    string
	userName string
}

// Клавиатуры диалога: выбора даты, части суток и цифровая панель значений.
var (
	dateKeyboard    = [][]string{{msgTodayButton}}
	dayPartKeyboard = [][]string{{msgMorningButton, msgDayButton, msgEveningButton}}
	digitKeyboard   = [][]string{
		{"7", "8", "9"},
		{"4", "5", "6"},
		{"1", "2", "3"},
		{"0", msgBackspace, msgSubmit},
	}
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
	case stateSystolic, stateDiastolic, stateHeartRate:
		return p.handleValue(ctx, chatID, userID, s, text)
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
	s.state = stateSystolic

	return p.sendValuePrompt(ctx, chatID, msgPromptSystolic)
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

// sendValuePrompt показывает промпт значения и цифровую панель.
func (p *Processor) sendValuePrompt(ctx context.Context, chatID int, prompt string) error {
	return p.tg.SendKeyboard(ctx, chatID, prompt, digitKeyboard, false)
}

// handleValue обрабатывает шаги ввода систолического, диастолического и пульса.
// Одиночная цифра копится в буфер s.value, «⌫» стирает, «Готово» и цельнocтное
// число текстом — отправляют значение на валидацию.
func (p *Processor) handleValue(ctx context.Context, chatID int, userID int64, s *session, text string) error {
	switch text {
	case msgBackspace:
		if len(s.value) > 0 {
			s.value = s.value[:len(s.value)-1]
		}
		return p.sendBufferMessage(ctx, chatID, s)
	case msgSubmit:
		if s.value == "" {
			return p.tg.SendMessage(ctx, chatID, msgInvalidNumber)
		}
		return p.submitValue(ctx, chatID, userID, s, s.value)
	default:
		if len(text) == 1 && isDigit(text[0]) {
			s.value += text
			return p.sendBufferMessage(ctx, chatID, s)
		}
		if isDigits(text) {
			return p.submitValue(ctx, chatID, userID, s, text)
		}

		return p.tg.SendMessage(ctx, chatID, msgInvalidNumber)
	}
}

// sendBufferMessage показывает текущий буфер цифровой панели или промпт,
// если буфер пуст.
func (p *Processor) sendBufferMessage(ctx context.Context, chatID int, s *session) error {
	if s.value == "" {
		return p.tg.SendMessage(ctx, chatID, promptFor(s.state))
	}

	return p.tg.SendMessage(ctx, chatID, fmt.Sprintf(msgValueBuffer, s.value))
}

func (p *Processor) submitValue(ctx context.Context, chatID int, userID int64, s *session, raw string) error {
	val, err := strconv.Atoi(raw)
	if err != nil {
		return p.tg.SendMessage(ctx, chatID, msgInvalidNumber)
	}

	switch s.state {
	case stateSystolic:
		if err := validateSystolic(val); err != nil {
			return p.tg.SendMessage(ctx, chatID, msgInvalidSystolic)
		}
		s.systolic = raw
		s.value = ""
		s.state = stateDiastolic

		return p.sendValuePrompt(ctx, chatID, msgPromptDiastolic)
	case stateDiastolic:
		if err := validateDiastolic(val); err != nil {
			return p.tg.SendMessage(ctx, chatID, msgInvalidDiastolic)
		}
		sys, _ := strconv.Atoi(s.systolic)
		if sys <= val {
			return p.tg.SendMessage(ctx, chatID, msgSystolicNotGreat)
		}
		s.diastolic = raw
		s.value = ""
		s.state = stateHeartRate

		return p.sendValuePrompt(ctx, chatID, msgPromptPulse)
	case stateHeartRate:
		if err := validateHeartRate(val); err != nil {
			return p.tg.SendMessage(ctx, chatID, msgInvalidPulse)
		}
		s.value = ""

		return p.completeDialog(ctx, chatID, userID, s, raw)
	default:
		return nil
	}
}

func (p *Processor) completeDialog(ctx context.Context, chatID int, userID int64, s *session, hr string) (err error) {
	defer func() {
		err = e.WrapIfErr("Ошибка при сохранении показаний давления", err)
	}()

	res, err := p.save(ctx, userID, s.userName, s.date, s.dayPart, s.systolic, s.diastolic, hr)
	p.cancelSession(userID)
	if err != nil {
		_ = p.tg.SendMessage(ctx, chatID, msgError)

		return err
	}

	if res == saveDuplicate {
		return p.tg.RemoveKeyboard(ctx, chatID, msgAlreadyExists)
	}

	return p.tg.RemoveKeyboard(ctx, chatID, msgSaved)
}

// cancelSession завершает активный диалог пользователя.
func (p *Processor) cancelSession(userID int64) {
	delete(p.sessions, userID)
}

// promptFor возвращает промпт шага ввода значения.
func promptFor(state dialogState) string {
	switch state {
	case stateSystolic:
		return msgPromptSystolic
	case stateDiastolic:
		return msgPromptDiastolic
	case stateHeartRate:
		return msgPromptPulse
	default:
		return ""
	}
}

func isDigit(b byte) bool {
	return b >= '0' && b <= '9'
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if !isDigit(s[i]) {
			return false
		}
	}

	return true
}
