package sqlite

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"blood-pressure-bot/storage"
)

func newTestStorage(t *testing.T) *Storage {
	t.Helper()

	path := filepath.Join(t.TempDir(), "test.db")

	s, err := New(path)
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}
	t.Cleanup(func() { _ = s.db.Close() })

	if err := s.Init(context.Background()); err != nil {
		t.Fatalf("Init() failed: %v", err)
	}

	return s
}

// today returns the current date in the storage timezone (matches Show's filter).
func today(t *testing.T) string {
	t.Helper()
	loc, err := time.LoadLocation("Asia/Yekaterinburg")
	if err != nil {
		t.Fatalf("LoadLocation failed: %v", err)
	}
	return time.Now().In(loc).Format("2006-01-02")
}

func TestInit_Idempotent(t *testing.T) {
	s := newTestStorage(t)

	if err := s.Init(context.Background()); err != nil {
		t.Errorf("second Init() failed: %v", err)
	}
}

func TestSave_IsExists(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	p := &storage.Pressure{
		Date:      today(t),
		DayPart:   "утро",
		Systolic:  "120",
		Diastolic: "80",
		HeartRate: "70",
		UserName:  "user1",
	}

	if err := s.Save(ctx, p); err != nil {
		t.Fatalf("Save() failed: %v", err)
	}

	ok, err := s.IsExists(ctx, p)
	if err != nil {
		t.Fatalf("IsExists() failed: %v", err)
	}
	if !ok {
		t.Error("IsExists() = false for saved record, want true")
	}

	// другой day_part → не существует
	otherPart := *p
	otherPart.DayPart = "вечер"
	ok, err = s.IsExists(ctx, &otherPart)
	if err != nil {
		t.Fatalf("IsExists(other part) failed: %v", err)
	}
	if ok {
		t.Error("IsExists() = true for different day_part, want false")
	}

	// другой user_name → не существует
	otherUser := *p
	otherUser.UserName = "user2"
	ok, err = s.IsExists(ctx, &otherUser)
	if err != nil {
		t.Fatalf("IsExists(other user) failed: %v", err)
	}
	if ok {
		t.Error("IsExists() = true for different user_name, want false")
	}
}

func TestShow_Today(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	date := today(t)
	p := &storage.Pressure{
		Date:      date,
		DayPart:   "утро",
		Systolic:  "120",
		Diastolic: "80",
		HeartRate: "70",
		UserName:  "user1",
	}
	if err := s.Save(ctx, p); err != nil {
		t.Fatalf("Save() failed: %v", err)
	}

	msg, err := s.Show(ctx, "user1")
	if err != nil {
		t.Fatalf("Show() failed: %v", err)
	}

	want := "Дата: " + date + ", часть суток: утро, показания: 120/80/70\n\n"
	if msg != want {
		t.Errorf("Show() = %q, want %q", msg, want)
	}

	if !strings.Contains(msg, "120/80/70") {
		t.Errorf("Show() output missing readings: %q", msg)
	}
}

func TestShow_OtherDay(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	// запись за вчера — не должна попасть в сегодняшнюю выборку
	p := &storage.Pressure{
		Date:      "2000-01-01",
		DayPart:   "утро",
		Systolic:  "120",
		Diastolic: "80",
		HeartRate: "70",
		UserName:  "user1",
	}
	if err := s.Save(ctx, p); err != nil {
		t.Fatalf("Save() failed: %v", err)
	}

	msg, err := s.Show(ctx, "user1")
	if err != nil {
		t.Fatalf("Show() failed: %v", err)
	}
	if msg != "" {
		t.Errorf("Show() = %q, want empty string (record is from another day)", msg)
	}
}

func TestShow_Empty(t *testing.T) {
	s := newTestStorage(t)

	msg, err := s.Show(context.Background(), "nobody")
	if err != nil {
		t.Errorf("Show() error = %v, want nil (Show never returns ErrNoSavedPressure)", err)
	}
	if msg != "" {
		t.Errorf("Show() = %q, want empty string", msg)
	}
}
