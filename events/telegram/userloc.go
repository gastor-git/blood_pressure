package telegram

import (
	"context"
	"log"
	"time"

	"blood-pressure-bot/lib/timeloc"
)

// userLoc возвращает локацию пользователя по закэшированному utc_offset
// (секунды от UTC). Нулевой/неизвестный offset — fallback на серверную таймзону.
func (p *Processor) userLoc(userID int64) *time.Location {
	off, ok := p.utcOffsets[userID]
	if !ok || off == 0 {
		return timeloc.Location()
	}

	return time.FixedZone("", off)
}

// userNow возвращает текущее время в локали пользователя. Таймзона
// подтягивается лениво через getChat, при недоступности — серверная.
func (p *Processor) userNow(ctx context.Context, chatID int, userID int64) time.Time {
	p.ensureTimezone(ctx, chatID, userID)

	return time.Now().In(p.userLoc(userID))
}

// userToday возвращает текущую дату пользователя в формате хранения.
func (p *Processor) userToday(ctx context.Context, chatID int, userID int64) string {
	return p.userNow(ctx, chatID, userID).Format(timeloc.DateFormat)
}

// ensureTimezone лениво запрашивает utc_offset пользователя через getChat и
// кэширует результат. Отличный от нуля offset сохраняется в БД (персист на
// рестарт). При ошибке offset не кэшируется — попытка повторится со следующим
// сообщением.
func (p *Processor) ensureTimezone(ctx context.Context, chatID int, userID int64) {
	if _, ok := p.utcOffsets[userID]; ok {
		return
	}

	info, err := p.tg.GetChat(ctx, chatID)
	if err != nil {
		// Ошибка уже санитизирована клиентом (токен не утечёт).
		log.Printf("не удалось получить таймзону пользователя %d: %s", userID, err.Error())

		return
	}

	p.utcOffsets[userID] = info.UTCOffset

	if info.UTCOffset != 0 {
		if err := p.storage.SetUTCOffset(ctx, userID, info.UTCOffset); err != nil {
			log.Printf("не удалось сохранить таймзону пользователя %d: %s", userID, err.Error())
		}
	}
}
