package telegram

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	tgClient "blood-pressure-bot/clients/telegram"
	"blood-pressure-bot/storage"
)

// --- mocks ---

type mockClient struct {
	sentChatID int
	sentTexts  []string
	sendErr    error
}

func (m *mockClient) Updates(offset, limit int) ([]tgClient.Update, error) {
	return nil, nil
}

func (m *mockClient) SendMessage(chatID int, text string) error {
	if m.sendErr != nil {
		return m.sendErr
	}
	m.sentChatID = chatID
	m.sentTexts = append(m.sentTexts, text)
	return nil
}

type mockStorage struct {
	saveFunc     func(ctx context.Context, p *storage.Pressure) error
	showFunc     func(ctx context.Context, userName string) (string, error)
	isExistsFunc func(ctx context.Context, p *storage.Pressure) (bool, error)

	saveCalls     int
	isExistsCalls int
	savedPressure *storage.Pressure
}

func (m *mockStorage) Save(ctx context.Context, p *storage.Pressure) error {
	m.saveCalls++
	m.savedPressure = p
	if m.saveFunc != nil {
		return m.saveFunc(ctx, p)
	}
	return nil
}

func (m *mockStorage) Show(ctx context.Context, userName string) (string, error) {
	if m.showFunc != nil {
		return m.showFunc(ctx, userName)
	}
	return "", nil
}

func (m *mockStorage) Remove(ctx context.Context, p *storage.Pressure) error {
	return nil
}

func (m *mockStorage) IsExists(ctx context.Context, p *storage.Pressure) (bool, error) {
	m.isExistsCalls++
	if m.isExistsFunc != nil {
		return m.isExistsFunc(ctx, p)
	}
	return false, nil
}

// --- tests ---

func TestIsPressure(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"120 80 70", true},
		{"90 60 50", true},
		{"100 100 100", true},
		{"999 999 999", true}, // диапазоны не валидируются — фиксируем текущее поведение
		{"120 80", false},
		{"120 80 70 60", false},
		{"1 2 3", false},
		{"1200 80 70", false},
		{"120  80 70", false}, // двойной пробел
		{"abc", false},
		{"", false},
	}

	for _, c := range cases {
		if got := isPressure(c.in); got != c.want {
			t.Errorf("isPressure(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestGetPressures(t *testing.T) {
	got := getPressures("120 80 70")
	want := []string{"120", "80", "70"}

	if !reflect.DeepEqual(got, want) {
		t.Errorf("getPressures() = %v, want %v", got, want)
	}
}

func TestDayPart(t *testing.T) {
	at := func(h, m int) time.Time {
		return time.Date(2026, 1, 2, h, m, 0, 0, time.UTC)
	}

	cases := []struct {
		t    time.Time
		want string
	}{
		{at(0, 0), "день"},  // БАГ: hour > 0 == false → полночь попадает в "день", фиксируем как есть
		{at(0, 59), "день"},
		{at(1, 0), "утро"},
		{at(12, 0), "утро"},
		{at(12, 59), "утро"}, // граница по часу, не по минуте
		{at(13, 0), "день"},
		{at(18, 0), "день"},
		{at(18, 59), "день"},
		{at(19, 0), "вечер"},
		{at(23, 59), "вечер"},
	}

	for _, c := range cases {
		if got := dayPart(c.t); got != c.want {
			t.Errorf("dayPart(%02d:%02d) = %q, want %q", c.t.Hour(), c.t.Minute(), got, c.want)
		}
	}
}

func TestDoCmd_Routing(t *testing.T) {
	cases := []struct {
		name string
		text string
		want string
	}{
		{"help", HelpCmd, msgHelp},
		{"start", StartCmd, msgHello},
		{"unknown", "/foo", msgUnknownCommand},
		{"help with spaces", "  /help  ", msgHelp},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			client := &mockClient{}
			p := New(client, &mockStorage{})

			if err := p.doCmd(c.text, 42, "user"); err != nil {
				t.Fatalf("doCmd returned error: %v", err)
			}

			if len(client.sentTexts) != 1 {
				t.Fatalf("expected 1 message, got %d", len(client.sentTexts))
			}
			if client.sentTexts[0] != c.want {
				t.Errorf("sent %q, want %q", client.sentTexts[0], c.want)
			}
			if client.sentChatID != 42 {
				t.Errorf("chatID = %d, want 42", client.sentChatID)
			}
		})
	}
}

func TestSavePressure_New(t *testing.T) {
	client := &mockClient{}
	st := &mockStorage{
		isExistsFunc: func(ctx context.Context, p *storage.Pressure) (bool, error) {
			return false, nil
		},
	}
	p := New(client, st)

	if err := p.doCmd("120 80 70", 42, "user"); err != nil {
		t.Fatalf("doCmd returned error: %v", err)
	}

	if st.saveCalls != 1 {
		t.Fatalf("Save called %d times, want 1", st.saveCalls)
	}

	saved := st.savedPressure
	if saved.Systolic != "120" || saved.Diastolic != "80" || saved.HeartRate != "70" {
		t.Errorf("saved pressures = %s/%s/%s, want 120/80/70", saved.Systolic, saved.Diastolic, saved.HeartRate)
	}
	if saved.UserName != "user" {
		t.Errorf("saved user = %q, want %q", saved.UserName, "user")
	}

	if len(client.sentTexts) != 1 || client.sentTexts[0] != msgSaved {
		t.Errorf("sent %v, want [%q]", client.sentTexts, msgSaved)
	}
}

func TestSavePressure_Duplicate(t *testing.T) {
	client := &mockClient{}
	st := &mockStorage{
		isExistsFunc: func(ctx context.Context, p *storage.Pressure) (bool, error) {
			return true, nil
		},
	}
	p := New(client, st)

	if err := p.doCmd("120 80 70", 42, "user"); err != nil {
		t.Fatalf("doCmd returned error: %v", err)
	}

	if st.saveCalls != 0 {
		t.Errorf("Save called %d times, want 0", st.saveCalls)
	}
	if len(client.sentTexts) != 1 || client.sentTexts[0] != msgAlreadyExists {
		t.Errorf("sent %v, want [%q]", client.sentTexts, msgAlreadyExists)
	}
}

func TestSavePressure_StorageError(t *testing.T) {
	sentinel := errors.New("boom")
	st := &mockStorage{
		isExistsFunc: func(ctx context.Context, p *storage.Pressure) (bool, error) {
			return false, nil
		},
		saveFunc: func(ctx context.Context, p *storage.Pressure) error {
			return sentinel
		},
	}
	p := New(&mockClient{}, st)

	err := p.doCmd("120 80 70", 42, "user")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, sentinel) {
		t.Errorf("error chain does not contain sentinel: %v", err)
	}
	if !strings.Contains(err.Error(), "Ошибка при сохранении показаний давления") {
		t.Errorf("error missing wrap prefix: %v", err)
	}
}

func TestShow_Empty(t *testing.T) {
	client := &mockClient{}
	st := &mockStorage{
		showFunc: func(ctx context.Context, userName string) (string, error) {
			return "", nil
		},
	}
	p := New(client, st)

	if err := p.doCmd(ShowCmd, 42, "user"); err != nil {
		t.Fatalf("doCmd returned error: %v", err)
	}

	if len(client.sentTexts) != 1 || client.sentTexts[0] != msgNoSavedPressure {
		t.Errorf("sent %v, want [%q]", client.sentTexts, msgNoSavedPressure)
	}
}

func TestShow_WithData(t *testing.T) {
	client := &mockClient{}
	const data = "Дата: 2026-01-02, часть суток: утро, показания: 120/80/70\n\n"
	st := &mockStorage{
		showFunc: func(ctx context.Context, userName string) (string, error) {
			return data, nil
		},
	}
	p := New(client, st)

	if err := p.doCmd(ShowCmd, 42, "user"); err != nil {
		t.Fatalf("doCmd returned error: %v", err)
	}

	if len(client.sentTexts) != 1 || client.sentTexts[0] != data {
		t.Errorf("sent %v, want [%q]", client.sentTexts, data)
	}
}
