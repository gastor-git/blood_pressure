// Package notifier рассылает пользователям напоминания о передаче показаний
// АД за 30 минут до окончания части суток (утро 11:30, день 17:30, вечер 23:30).
package notifier

import (
	"context"
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
	UsersWithoutPressure(ctx context.Context, date, dayPart string) ([]storage.User, error)
}

// Sender — отправка сообщений на стороне потребителя.
type Sender interface {
	SendMessage(ctx context.Context, chatID int, text string) error
}

// Notifier рассылает напоминания по расписанию.
type Notifier struct {
	storage Storage
	tg      Sender
}

func New(storage Storage, tg Sender) *Notifier {
	return &Notifier{
		storage: storage,
		tg:      tg,
	}
}

// reminderHours — часы срабатывания напоминаний (минута всегда 30).
var reminderHours = []int{11, 17, 23}

const reminderMinute = 30

// nextTrigger возвращает ближайшее время срабатывания из {11:30, 17:30, 23:30}
// строго позже now; если все три сегодня уже прошли — первое из них завтра.
func nextTrigger(now time.Time) time.Time {
	now = now.In(timeloc.Location())

	for _, h := range reminderHours {
		candidate := time.Date(now.Year(), now.Month(), now.Day(), h, reminderMinute, 0, 0, timeloc.Location())
		if candidate.After(now) {
			return candidate
		}
	}

	tomorrow := now.AddDate(0, 0, 1)
	return time.Date(tomorrow.Year(), tomorrow.Month(), tomorrow.Day(), reminderHours[0], reminderMinute, 0, 0, timeloc.Location())
}

// Start запускает цикл рассылки; завершается по отмене ctx (как консьюмер).
// Пропущенный триггер не догоняется: каждый раз берётся ближайшее будущее время.
func (n *Notifier) Start(ctx context.Context) error {
	for {
		trigger := nextTrigger(timeloc.Now())
		timer := time.NewTimer(time.Until(trigger))

		select {
		case <-ctx.Done():
			timer.Stop()
			return nil
		case <-timer.C:
		}

		if err := n.notify(ctx, trigger); err != nil {
			log.Printf("[ERR] notifier: %s", err.Error())
		}
	}
}

// notify рассылает напоминания пользователям, ещё не передавшим показания за
// текущую часть суток. Сбой отправки одному пользователю не прерывает рассылку.
func (n *Notifier) notify(ctx context.Context, trigger time.Time) (err error) {
	defer func() {
		err = e.WrapIfErr("не удалось разослать напоминания", err)
	}()

	label := telegram.DayPart(trigger)

	users, err := n.storage.UsersWithoutPressure(ctx, timeloc.Today(), label)
	if err != nil {
		return err
	}

	for _, u := range users {
		if err := n.tg.SendMessage(ctx, int(u.ChatID), fmt.Sprintf(telegram.MsgReminder, label)); err != nil {
			log.Printf("[ERR] notifier: не удалось отправить напоминание пользователю %d: %s", u.UserID, err.Error())
		}
	}

	return nil
}
