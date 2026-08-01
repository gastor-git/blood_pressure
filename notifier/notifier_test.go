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
	users   []storage.User
	err     error
	gotDate string
	gotPart string
}

func (m *mockStorage) UsersWithoutPressure(ctx context.Context, date, dayPart string) ([]storage.User, error) {
	m.gotDate = date
	m.gotPart = dayPart
	return m.users, m.err
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
			if got := nextTrigger(c.now); !got.Equal(c.want) {
				t.Errorf("nextTrigger(%v) = %v, want %v", c.now, got, c.want)
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
	if st.gotDate != timeloc.Today() {
		t.Errorf("date for storage = %q, want %q", st.gotDate, timeloc.Today())
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
