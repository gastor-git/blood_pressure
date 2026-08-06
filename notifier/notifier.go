// Package notifier рассылает пользователям напоминания о передаче показаний
// АД за 30 минут до окончания части суток (утро 11:30, день 17:30, вечер 23:30)
// в персональной таймзоне пользователя (utc_offset), fallback — серверная ТЗ.
package notifier

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"blood-pressure-bot/events/telegram"
	"blood-pressure-bot/lib/e"
	"blood-pressure-bot/lib/timeloc"
	"blood-pressure-bot/storage"
)

// Storage — выбор получателей на стороне потребителя.
type Storage interface {
	AllUsers(ctx context.Context) ([]storage.User, error)
	Get(ctx context.Context, userID int64, date, dayPart string) (*storage.Pressure, error)
}

// Sender — отправка сообщений на стороне потребителя.
type Sender interface {
	SendMessage(ctx context.Context, chatID int, text string) error
}

// Notifier рассылает напоминания по расписанию.
type Notifier struct {
	storage Storage
	tg      Sender
	// now — источник текущего времени (переопределяется в тестах).
	now func() time.Time
}

func New(storage Storage, tg Sender) *Notifier {
	return &Notifier{
		storage: storage,
		tg:      tg,
		now:     time.Now,
	}
}

// reminderHours — часы срабатывания напоминаний (минута всегда 30).
var reminderHours = []int{11, 17, 23}

const reminderMinute = 30

// nextTrigger возвращает ближайшее время срабатывания из {11:30, 17:30, 23:30}
// строго позже now в локации loc; если все три сегодня уже прошли — первое из
// них завтра.
func nextTrigger(now time.Time, loc *time.Location) time.Time {
	now = now.In(loc)

	for _, h := range reminderHours {
		candidate := time.Date(now.Year(), now.Month(), now.Day(), h, reminderMinute, 0, 0, loc)
		if candidate.After(now) {
			return candidate
		}
	}

	tomorrow := now.AddDate(0, 0, 1)
	return time.Date(tomorrow.Year(), tomorrow.Month(), tomorrow.Day(), reminderHours[0], reminderMinute, 0, 0, loc)
}

// userLoc возвращает локацию пользователя по utc_offset (секунды от UTC).
// Нулевой offset (неизвестен) — fallback на серверную таймзону.
func userLoc(offset int) *time.Location {
	if offset == 0 {
		return timeloc.Location()
	}

	return time.FixedZone("", offset)
}

// isReminderMinute возвращает true, если время t попадает в минуту срабатывания
// напоминания (час из reminderHours и минута reminderMinute) в локали t.
func isReminderMinute(t time.Time) bool {
	if t.Minute() != reminderMinute {
		return false
	}

	for _, h := range reminderHours {
		if t.Hour() == h {
			return true
		}
	}

	return false
}

// nextTriggerAll возвращает ближайшее срабатывание напоминания с учётом
// персональных таймзон всех пользователей (минимум по nextTrigger). При пустом
// списке или ошибке выборки — fallback на серверную таймзону, чтобы цикл не
// крутился вхолостую.
func (n *Notifier) nextTriggerAll(ctx context.Context) time.Time {
	now := n.now()

	users, err := n.storage.AllUsers(ctx)
	if err != nil || len(users) == 0 {
		if err != nil {
			log.Printf("[ERR] notifier: не удалось получить пользователей: %s", err.Error())
		}

		return nextTrigger(now, timeloc.Location())
	}

	min := nextTrigger(now, userLoc(users[0].UTCOffset))
	for _, u := range users[1:] {
		if t := nextTrigger(now, userLoc(u.UTCOffset)); t.Before(min) {
			min = t
		}
	}

	return min
}

// Start запускает цикл рассылки; завершается по отмене ctx (как консьюмер).
// Пропущенный триггер не догоняется: каждый раз берётся ближайшее будущее время.
func (n *Notifier) Start(ctx context.Context) error {
	for {
		trigger := n.nextTriggerAll(ctx)
		timer := time.NewTimer(time.Until(trigger))

		select {
		case <-ctx.Done():
			timer.Stop()
			return nil
		case <-timer.C:
		}

		if err := n.notify(ctx, n.now()); err != nil {
			log.Printf("[ERR] notifier: %s", err.Error())
		}
	}
}

// retryAfter — ошибка, которая сообщает рекомендованную паузу перед повтором
// (секунды). Реализуется *telegram.APIError.
type retryAfter interface {
	RetryAfter() int
}

// retryAfterDelay возвращает рекомендованную паузу для ошибки (0 — пауза не
// нужна).
func retryAfterDelay(err error) time.Duration {
	var ra retryAfter
	if errors.As(err, &ra) && ra.RetryAfter() > 0 {
		return time.Duration(ra.RetryAfter()) * time.Second
	}

	return 0
}

// backoffPause при ошибке отправки с retry_after ждёт рекомендованную паузу,
// чтобы не спамить Telegram при 429. Возвращает без ожидания, если пауза не
// нужна или контекст отменён.
func backoffPause(ctx context.Context, err error) {
	d := retryAfterDelay(err)
	if d == 0 {
		return
	}

	select {
	case <-ctx.Done():
	case <-time.After(d):
	}
}

// notify рассылает напоминания пользователям, у которых локально наступил
// триггер и нет записи за текущую часть суток их дня. Сбой отправки или
// выборки записи одному пользователю не прерывает рассылку.
func (n *Notifier) notify(ctx context.Context, now time.Time) (err error) {
	defer func() {
		err = e.WrapIfErr("не удалось разослать напоминания", err)
	}()

	users, err := n.storage.AllUsers(ctx)
	if err != nil {
		return err
	}

	for _, u := range users {
		local := now.In(userLoc(u.UTCOffset))
		if !isReminderMinute(local) {
			continue
		}

		date := local.Format(timeloc.DateFormat)
		label := telegram.DayPart(local)

		rec, err := n.storage.Get(ctx, u.UserID, date, label)
		if err != nil {
			log.Printf("[ERR] notifier: не удалось проверить запись пользователя %d: %s", u.UserID, err.Error())
			continue
		}
		if rec != nil {
			continue
		}

		if err := n.tg.SendMessage(ctx, int(u.ChatID), fmt.Sprintf(telegram.MsgReminder, label)); err != nil {
			log.Printf("[ERR] notifier: не удалось отправить напоминание пользователю %d: %s", u.UserID, err.Error())
			backoffPause(ctx, err)
		}
	}

	return nil
}
