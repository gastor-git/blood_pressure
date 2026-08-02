package notifier

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"blood-pressure-bot/events/telegram"
	"blood-pressure-bot/lib/timeloc"
	"blood-pressure-bot/storage"
)

type mockStorage struct {
	users     []storage.User
	err       error
	record    *storage.Pressure
	gotUserID int64
	gotDate   string
	gotPart   string
}

func (m *mockStorage) AllUsers(ctx context.Context) ([]storage.User, error) {
	return m.users, m.err
}

func (m *mockStorage) Get(ctx context.Context, userID int64, date, dayPart string) (*storage.Pressure, error) {
	m.gotUserID = userID
	m.gotDate = date
	m.gotPart = dayPart
	if m.record != nil {
		return m.record, nil
	}
	return nil, nil
}

type mockSender struct {
	chatIDs []int
	texts   []string
	fail    map[int]bool
}

func (m *mockSender) SendMessage(ctx context.Context, chatID int, text string) error {
	m.chatIDs = append(m.chatIDs, chatID)
	m.texts = append(m.texts, text)
	if m.fail[chatID] {
		return errors.New("send boom")
	}
	return nil
}

func TestNextTrigger(t *testing.T) {
	at := func(h, m int) time.Time {
		return time.Date(2026, 1, 2, h, m, 0, 0, timeloc.Location())
	}
	nextDay := func(h, m int) time.Time {
		return time.Date(2026, 1, 3, h, m, 0, 0, timeloc.Location())
	}

	cases := []struct {
		name string
		now  time.Time
		want time.Time
	}{
		{"до 11:30", at(10, 0), at(11, 30)},
		{"12:00 → 17:30", at(12, 0), at(17, 30)},
		{"18:00 → 23:30", at(18, 0), at(23, 30)},
		{"23:31 → 11:30 завтра", at(23, 31), nextDay(11, 30)},
		{"ровно 11:30 → 17:30", at(11, 30), at(17, 30)},
		{"ровно 17:30 → 23:30", at(17, 30), at(23, 30)},
		{"ровно 23:30 → завтра 11:30", at(23, 30), nextDay(11, 30)},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := nextTrigger(c.now, timeloc.Location()); !got.Equal(c.want) {
				t.Errorf("nextTrigger(%v) = %v, want %v", c.now, got, c.want)
			}
		})
	}
}

func TestNextTrigger_UserLoc(t *testing.T) {
	loc := time.FixedZone("", 14*3600)
	now := time.Date(2026, 1, 2, 23, 0, 0, 0, loc)
	want := time.Date(2026, 1, 2, 23, 30, 0, 0, loc)

	if got := nextTrigger(now, loc); !got.Equal(want) {
		t.Errorf("nextTrigger() = %v, want %v", got, want)
	}
}

func TestUserLoc(t *testing.T) {
	if userLoc(0) != timeloc.Location() {
		t.Error("userLoc(0) must fall back to server location")
	}

	now := time.Date(2026, 1, 2, 12, 0, 0, 0, time.UTC)
	got := now.In(userLoc(14 * 3600))
	if _, off := got.Zone(); off != 14*3600 {
		t.Errorf("userLoc(14h) zone offset = %d, want %d", off, 14*3600)
	}
	if got.Hour() != 2 {
		t.Errorf("userLoc(14h) local hour = %d, want 2", got.Hour())
	}
}

func TestIsReminderMinute(t *testing.T) {
	at := func(h, m int) time.Time {
		return time.Date(2026, 1, 2, h, m, 0, 0, time.UTC)
	}

	cases := []struct {
		name string
		t    time.Time
		want bool
	}{
		{"11:30", at(11, 30), true},
		{"17:30", at(17, 30), true},
		{"23:30", at(23, 30), true},
		{"11:29", at(11, 29), false},
		{"11:31", at(11, 31), false},
		{"12:30", at(12, 30), false},
		{"23:00", at(23, 0), false},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := isReminderMinute(c.t); got != c.want {
				t.Errorf("isReminderMinute(%v) = %v, want %v", c.t, got, c.want)
			}
		})
	}
}

func TestNotify(t *testing.T) {
	st := &mockStorage{
		users: []storage.User{
			{UserID: 1, ChatID: 100, UserName: "alice"},
			{UserID: 2, ChatID: 200, UserName: "bob"},
		},
	}
	sender := &mockSender{}
	n := New(st, sender)

	trigger := time.Date(2026, 1, 2, 17, 30, 0, 0, timeloc.Location())
	if err := n.notify(context.Background(), trigger); err != nil {
		t.Fatalf("notify() error: %v", err)
	}

	if st.gotPart != "день" {
		t.Errorf("dayPart for storage = %q, want %q", st.gotPart, "день")
	}
	if st.gotDate != "2026-01-02" {
		t.Errorf("date for storage = %q, want %q", st.gotDate, "2026-01-02")
	}

	want := fmt.Sprintf(telegram.MsgReminder, "день")
	if len(sender.chatIDs) != 2 {
		t.Fatalf("sent to %d users, want 2", len(sender.chatIDs))
	}
	for _, text := range sender.texts {
		if text != want {
			t.Errorf("sent %q, want %q", text, want)
		}
	}
	if sender.chatIDs[0] != 100 || sender.chatIDs[1] != 200 {
		t.Errorf("chatIDs = %v, want [100 200]", sender.chatIDs)
	}
}

func TestNotify_PerUserTimezones(t *testing.T) {
	// 12:30 UTC = 17:30 в серверной таймзоне (UTC+5) — триггер дня.
	now := time.Date(2026, 1, 2, 12, 30, 0, 0, time.UTC)

	st := &mockStorage{
		users: []storage.User{
			{UserID: 1, ChatID: 100, UserName: "ekb-fallback"},                     // offset 0 → 17:30 — день
			{UserID: 2, ChatID: 200, UserName: "far-east", UTCOffset: 14 * 3600},   // 02:30 следующего дня — не триггер
			{UserID: 3, ChatID: 300, UserName: "west", UTCOffset: -12 * 3600},      // 00:30 — не триггер
			{UserID: 4, ChatID: 400, UserName: "kiritimati", UTCOffset: 11 * 3600}, // 23:30 — вечер
		},
	}
	sender := &mockSender{}
	n := New(st, sender)

	if err := n.notify(context.Background(), now); err != nil {
		t.Fatalf("notify() error: %v", err)
	}

	if len(sender.chatIDs) != 2 {
		t.Fatalf("sent to %d users, want 2 (только локальные триггеры)", len(sender.chatIDs))
	}
	if sender.chatIDs[0] != 100 || sender.chatIDs[1] != 400 {
		t.Errorf("chatIDs = %v, want [100 400]", sender.chatIDs)
	}
	if want := fmt.Sprintf(telegram.MsgReminder, "день"); sender.texts[0] != want {
		t.Errorf("first text = %q, want %q", sender.texts[0], want)
	}
	if want := fmt.Sprintf(telegram.MsgReminder, "вечер"); sender.texts[1] != want {
		t.Errorf("second text = %q, want %q", sender.texts[1], want)
	}
}

func TestNotify_AlreadyRecorded(t *testing.T) {
	st := &mockStorage{
		users: []storage.User{
			{UserID: 1, ChatID: 100, UserName: "alice"},
		},
		record: &storage.Pressure{Date: "2026-01-02", DayPart: "день"},
	}
	sender := &mockSender{}
	n := New(st, sender)

	trigger := time.Date(2026, 1, 2, 17, 30, 0, 0, timeloc.Location())
	if err := n.notify(context.Background(), trigger); err != nil {
		t.Fatalf("notify() error: %v", err)
	}

	if len(sender.chatIDs) != 0 {
		t.Fatalf("sent to %d users, want 0 (запись уже есть)", len(sender.chatIDs))
	}
	if st.gotPart != "день" {
		t.Errorf("dayPart for storage = %q, want %q", st.gotPart, "день")
	}
}

func TestNotify_StorageError(t *testing.T) {
	sentinel := errors.New("storage boom")
	st := &mockStorage{err: sentinel}
	sender := &mockSender{}
	n := New(st, sender)

	trigger := time.Date(2026, 1, 2, 17, 30, 0, 0, timeloc.Location())
	err := n.notify(context.Background(), trigger)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, sentinel) {
		t.Errorf("error chain does not contain sentinel: %v", err)
	}
	if len(sender.chatIDs) != 0 {
		t.Errorf("sent %d messages, want 0", len(sender.chatIDs))
	}
}

func TestNotify_SendError(t *testing.T) {
	st := &mockStorage{
		users: []storage.User{
			{UserID: 1, ChatID: 100, UserName: "alice"},
			{UserID: 2, ChatID: 200, UserName: "bob"},
		},
	}
	sender := &mockSender{fail: map[int]bool{100: true}}
	n := New(st, sender)

	trigger := time.Date(2026, 1, 2, 17, 30, 0, 0, timeloc.Location())
	if err := n.notify(context.Background(), trigger); err != nil {
		t.Fatalf("notify() error: %v", err)
	}

	if len(sender.chatIDs) != 2 {
		t.Fatalf("sent to %d users, want 2 (сбой одного не прерывает рассылку)", len(sender.chatIDs))
	}
	if sender.chatIDs[0] != 100 || sender.chatIDs[1] != 200 {
		t.Errorf("chatIDs = %v, want [100 200]", sender.chatIDs)
	}
}

func TestNextTriggerAll_FallbackOnError(t *testing.T) {
	st := &mockStorage{err: errors.New("boom")}
	n := New(st, &mockSender{})

	got := n.nextTriggerAll(context.Background())
	// при ошибке — ближайший триггер в серверной таймзоне
	want := nextTrigger(time.Now(), timeloc.Location())
	if got.Unix() != want.Unix() {
		t.Errorf("nextTriggerAll() = %v, want %v (fallback серверная ТЗ)", got, want)
	}
}

// retryErr — ошибка с рекомендацией паузы (имитация *telegram.APIError).
type retryErr struct {
	seconds int
}

func (e retryErr) Error() string { return "rate limited" }

func (e retryErr) RetryAfter() int { return e.seconds }

func TestRetryAfterDelay(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want time.Duration
	}{
		{"без retry_after", errors.New("plain"), 0},
		{"nil", nil, 0},
		{"retry_after 3с", retryErr{seconds: 3}, 3 * time.Second},
		{"retry_after 0 — не нужна", retryErr{seconds: 0}, 0},
		{"обёрнутая ошибка", fmt.Errorf("wrap: %w", retryErr{seconds: 5}), 5 * time.Second},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := retryAfterDelay(c.err); got != c.want {
				t.Errorf("retryAfterDelay() = %v, want %v", got, c.want)
			}
		})
	}
}

func TestBackoffPause_CancelledContext(t *testing.T) {
	// отменённый контекст прерывает ожидание — не должно зависнуть
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	start := time.Now()
	backoffPause(ctx, retryErr{seconds: 30})
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("backoffPause() with cancelled ctx took %v, want immediate return", elapsed)
	}
}
