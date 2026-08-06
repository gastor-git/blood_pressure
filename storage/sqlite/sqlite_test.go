package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"blood-pressure-bot/lib/timeloc"
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

// today возвращает текущую дату в таймзоне учёта (совпадает с фильтром Show).
// Зависит от timeloc.Location(), а не от хардкода — смена таймзоны в проде
// не ломает тесты.
func today(t *testing.T) string {
	t.Helper()
	return timeloc.Today()
}

func TestInit_Idempotent(t *testing.T) {
	s := newTestStorage(t)

	if err := s.Init(context.Background()); err != nil {
		t.Errorf("second Init() failed: %v", err)
	}
}

func TestSave_Duplicate(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	p := &storage.Pressure{
		Date:      today(t),
		DayPart:   "утро",
		Systolic:  "120",
		Diastolic: "80",
		HeartRate: "70",
		UserID:    1,
		UserName:  "user1",
	}

	saved, err := s.Save(ctx, p)
	if err != nil {
		t.Fatalf("Save() failed: %v", err)
	}
	if !saved {
		t.Fatal("first Save() = false, want true")
	}

	// повторная запись за ту же часть суток — false, дубликат не создаётся
	saved, err = s.Save(ctx, p)
	if err != nil {
		t.Fatalf("second Save() failed: %v", err)
	}
	if saved {
		t.Error("second Save() = true, want false (duplicate)")
	}

	res, err := s.Show(ctx, 1, today(t))
	if err != nil {
		t.Fatalf("Show() failed: %v", err)
	}
	if len(res) != 1 {
		t.Errorf("Show() returned %d rows, want 1", len(res))
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
		UserID:    1,
		UserName:  "user1",
	}
	if _, err := s.Save(ctx, p); err != nil {
		t.Fatalf("Save() failed: %v", err)
	}

	res, err := s.Show(ctx, 1, date)
	if err != nil {
		t.Fatalf("Show() failed: %v", err)
	}

	if len(res) != 1 {
		t.Fatalf("Show() returned %d rows, want 1", len(res))
	}
	got := res[0]
	if got.Date != date || got.DayPart != "утро" || got.Systolic != "120" || got.Diastolic != "80" || got.HeartRate != "70" {
		t.Errorf("Show() = %+v, want date=%s day_part=утро 120/80/70", got, date)
	}
}

func TestShow_ByUserID(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()
	date := today(t)

	// две записи с одинаковым (пустым) user_name, но разными user_id
	p1 := &storage.Pressure{Date: date, DayPart: "утро", Systolic: "120", Diastolic: "80", HeartRate: "70", UserID: 1}
	p2 := &storage.Pressure{Date: date, DayPart: "утро", Systolic: "130", Diastolic: "85", HeartRate: "75", UserID: 2}

	if _, err := s.Save(ctx, p1); err != nil {
		t.Fatalf("Save(p1) failed: %v", err)
	}
	if _, err := s.Save(ctx, p2); err != nil {
		t.Fatalf("Save(p2) failed: %v", err)
	}

	res1, err := s.Show(ctx, 1, date)
	if err != nil {
		t.Fatalf("Show(1) failed: %v", err)
	}
	if len(res1) != 1 || res1[0].Systolic != "120" {
		t.Errorf("Show(1) = %+v, want single record 120/...", res1)
	}

	res2, err := s.Show(ctx, 2, date)
	if err != nil {
		t.Fatalf("Show(2) failed: %v", err)
	}
	if len(res2) != 1 || res2[0].Systolic != "130" {
		t.Errorf("Show(2) = %+v, want single record 130/...", res2)
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
		UserID:    1,
		UserName:  "user1",
	}
	if _, err := s.Save(ctx, p); err != nil {
		t.Fatalf("Save() failed: %v", err)
	}

	res, err := s.Show(ctx, 1, today(t))
	if err != nil {
		t.Fatalf("Show() failed: %v", err)
	}
	if len(res) != 0 {
		t.Errorf("Show() = %+v, want empty (record is from another day)", res)
	}
}

func TestShow_Empty(t *testing.T) {
	s := newTestStorage(t)

	res, err := s.Show(context.Background(), 999, today(t))
	if err != nil {
		t.Errorf("Show() error = %v, want nil", err)
	}
	if len(res) != 0 {
		t.Errorf("Show() = %+v, want empty slice", res)
	}
}

func TestGet_Found(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()
	date := today(t)

	p := &storage.Pressure{Date: date, DayPart: "утро", Systolic: "120", Diastolic: "80", HeartRate: "70", UserID: 1, UserName: "user1"}
	if _, err := s.Save(ctx, p); err != nil {
		t.Fatalf("Save() failed: %v", err)
	}

	got, err := s.Get(ctx, 1, date, "утро")
	if err != nil {
		t.Fatalf("Get() failed: %v", err)
	}
	if got == nil {
		t.Fatal("Get() = nil, want record")
	}
	if got.Date != date || got.DayPart != "утро" || got.Systolic != "120" || got.Diastolic != "80" || got.HeartRate != "70" {
		t.Errorf("Get() = %+v, want date=%s утро 120/80/70", got, date)
	}
	if got.UserID != 1 || got.UserName != "user1" {
		t.Errorf("Get() user = %d/%q, want 1/user1", got.UserID, got.UserName)
	}
}

func TestGet_NotFound(t *testing.T) {
	s := newTestStorage(t)

	got, err := s.Get(context.Background(), 999, today(t), "утро")
	if err != nil {
		t.Fatalf("Get() error = %v, want nil", err)
	}
	if got != nil {
		t.Errorf("Get() = %+v, want nil", got)
	}
}

func TestGet_OtherUser(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()
	date := today(t)

	p := &storage.Pressure{Date: date, DayPart: "утро", Systolic: "120", Diastolic: "80", HeartRate: "70", UserID: 1, UserName: "user1"}
	if _, err := s.Save(ctx, p); err != nil {
		t.Fatalf("Save() failed: %v", err)
	}

	// тот же день и часть суток, но другой пользователь
	got, err := s.Get(ctx, 2, date, "утро")
	if err != nil {
		t.Fatalf("Get() failed: %v", err)
	}
	if got != nil {
		t.Errorf("Get(2) = %+v, want nil (record belongs to user 1)", got)
	}
}

func TestUpdate_Overwrite(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()
	date := today(t)

	orig := &storage.Pressure{Date: date, DayPart: "утро", Systolic: "120", Diastolic: "80", HeartRate: "70", UserID: 1, UserName: "user1"}
	if _, err := s.Save(ctx, orig); err != nil {
		t.Fatalf("Save() failed: %v", err)
	}

	upd := &storage.Pressure{Date: date, DayPart: "утро", Systolic: "130", Diastolic: "85", HeartRate: "75", UserID: 1, UserName: "user1_renamed"}
	if err := s.Update(ctx, upd); err != nil {
		t.Fatalf("Update() failed: %v", err)
	}

	// ключ не изменился: запись по-прежнему одна, значения перезаписаны
	res, err := s.GetAll(ctx, 1)
	if err != nil {
		t.Fatalf("GetAll() failed: %v", err)
	}
	if len(res) != 1 {
		t.Fatalf("GetAll() returned %d rows, want 1", len(res))
	}
	got := res[0]
	if got.Systolic != "130" || got.Diastolic != "85" || got.HeartRate != "75" {
		t.Errorf("GetAll() = %+v, want updated 130/85/75", got)
	}
	if got.UserName != "user1_renamed" {
		t.Errorf("UserName = %q, want user1_renamed", got.UserName)
	}
}

func TestUpdate_NotFound(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	upd := &storage.Pressure{Date: today(t), DayPart: "утро", Systolic: "130", Diastolic: "85", HeartRate: "75", UserID: 999}
	err := s.Update(ctx, upd)
	if err == nil {
		t.Fatal("Update() = nil, want error for missing record")
	}
	if !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("Update() error = %v, want sql.ErrNoRows", err)
	}
}

func TestGetAll_AllTime(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	// записи за разные даты (включая не-сегодняшнюю)
	for _, d := range []string{"2000-01-01", "2000-01-02", today(t)} {
		p := &storage.Pressure{Date: d, DayPart: "утро", Systolic: "120", Diastolic: "80", HeartRate: "70", UserID: 1, UserName: "user1"}
		if _, err := s.Save(ctx, p); err != nil {
			t.Fatalf("Save() failed: %v", err)
		}
	}

	res, err := s.GetAll(ctx, 1)
	if err != nil {
		t.Fatalf("GetAll() failed: %v", err)
	}
	if len(res) != 3 {
		t.Fatalf("GetAll() returned %d rows, want 3", len(res))
	}
	// упорядочены по дате (лексикографически = хронологически)
	for i, d := range []string{"2000-01-01", "2000-01-02", today(t)} {
		if res[i].Date != d {
			t.Errorf("res[%d].Date = %q, want %q", i, res[i].Date, d)
		}
	}
}

func TestGetAll_ByUser(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	p1 := &storage.Pressure{Date: "2000-01-01", DayPart: "утро", Systolic: "120", Diastolic: "80", HeartRate: "70", UserID: 1}
	p2 := &storage.Pressure{Date: "2000-01-01", DayPart: "утро", Systolic: "130", Diastolic: "85", HeartRate: "75", UserID: 2}

	if _, err := s.Save(ctx, p1); err != nil {
		t.Fatalf("Save(p1) failed: %v", err)
	}
	if _, err := s.Save(ctx, p2); err != nil {
		t.Fatalf("Save(p2) failed: %v", err)
	}

	res, err := s.GetAll(ctx, 1)
	if err != nil {
		t.Fatalf("GetAll() failed: %v", err)
	}
	if len(res) != 1 || res[0].Systolic != "120" {
		t.Errorf("GetAll(1) = %+v, want single record 120/...", res)
	}
}

func TestGetAll_Empty(t *testing.T) {
	s := newTestStorage(t)

	res, err := s.GetAll(context.Background(), 999)
	if err != nil {
		t.Errorf("GetAll() error = %v, want nil", err)
	}
	if len(res) != 0 {
		t.Errorf("GetAll() = %+v, want empty slice", res)
	}
}

func TestClaimLegacy(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()
	date := today(t)

	// legacy-строка без user_id
	if err := insertLegacy(ctx, s, date, "утро", "120", "80", "70", "user1"); err != nil {
		t.Fatalf("insertLegacy failed: %v", err)
	}

	// до привязки запись не видна по user_id
	res, err := s.Show(ctx, 1, date)
	if err != nil {
		t.Fatalf("Show() failed: %v", err)
	}
	if len(res) != 0 {
		t.Fatalf("Show() before claim = %+v, want empty", res)
	}

	if err := s.ClaimLegacy(ctx, 1, "user1"); err != nil {
		t.Fatalf("ClaimLegacy() failed: %v", err)
	}

	res, err = s.Show(ctx, 1, date)
	if err != nil {
		t.Fatalf("Show() after claim failed: %v", err)
	}
	if len(res) != 1 || res[0].Systolic != "120" {
		t.Errorf("Show() after claim = %+v, want single record 120/...", res)
	}
}

func TestClaimLegacy_OtherUserUntouched(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()
	date := today(t)

	if err := insertLegacy(ctx, s, date, "утро", "120", "80", "70", "user1"); err != nil {
		t.Fatalf("insertLegacy(user1) failed: %v", err)
	}
	if err := insertLegacy(ctx, s, date, "утро", "130", "85", "75", "user2"); err != nil {
		t.Fatalf("insertLegacy(user2) failed: %v", err)
	}

	// привязываем только user1
	if err := s.ClaimLegacy(ctx, 1, "user1"); err != nil {
		t.Fatalf("ClaimLegacy() failed: %v", err)
	}

	// user2 остался непривязанным — его запись не видна ни под чьим user_id
	res, err := s.Show(ctx, 2, today(t))
	if err != nil {
		t.Fatalf("Show(2) failed: %v", err)
	}
	if len(res) != 0 {
		t.Errorf("Show(2) = %+v, want empty (user2 not claimed)", res)
	}
}

// insertLegacy вставляет запись без user_id (эмуляция старой строки).
func insertLegacy(ctx context.Context, s *Storage, date, dayPart, sys, dia, hr, userName string) error {
	q := `INSERT INTO blood_pressure (date, day_part, systolic, diastolic, heart_rate, user_name) VALUES (?, ?, ?, ?, ?, ?)`
	_, err := s.db.ExecContext(ctx, q, date, dayPart, sys, dia, hr, userName)
	return err
}

func TestRegisterUser_Insert(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	if err := s.RegisterUser(ctx, 1, 42, "user1"); err != nil {
		t.Fatalf("RegisterUser() failed: %v", err)
	}

	users, err := s.AllUsers(ctx)
	if err != nil {
		t.Fatalf("AllUsers() failed: %v", err)
	}
	if len(users) != 1 {
		t.Fatalf("AllUsers() returned %d users, want 1", len(users))
	}
	got := users[0]
	if got.UserID != 1 || got.ChatID != 42 || got.UserName != "user1" {
		t.Errorf("AllUsers() = %+v, want {1 42 user1}", got)
	}
	if got.UTCOffset != 0 {
		t.Errorf("UTCOffset = %d, want 0 (default)", got.UTCOffset)
	}
}

func TestRegisterUser_Upsert(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	if err := s.RegisterUser(ctx, 1, 42, "user1"); err != nil {
		t.Fatalf("RegisterUser() failed: %v", err)
	}
	// повторный вызов обновляет chat_id/user_name, дубликат не создаётся
	if err := s.RegisterUser(ctx, 1, 43, "user_renamed"); err != nil {
		t.Fatalf("second RegisterUser() failed: %v", err)
	}

	var count int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&count); err != nil {
		t.Fatalf("count users failed: %v", err)
	}
	if count != 1 {
		t.Errorf("users count = %d, want 1", count)
	}

	users, err := s.AllUsers(ctx)
	if err != nil {
		t.Fatalf("AllUsers() failed: %v", err)
	}
	if len(users) != 1 {
		t.Fatalf("AllUsers() returned %d users, want 1", len(users))
	}
	got := users[0]
	if got.ChatID != 43 || got.UserName != "user_renamed" {
		t.Errorf("AllUsers() = %+v, want updated {1 43 user_renamed}", got)
	}
}

func TestAllUsers(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	// трое зарегистрированных пользователей
	for _, u := range []struct {
		id   int64
		chat int64
		name string
	}{
		{1, 100, "alice"},
		{2, 200, "bob"},
		{3, 300, "carol"},
	} {
		if err := s.RegisterUser(ctx, u.id, u.chat, u.name); err != nil {
			t.Fatalf("RegisterUser(%d) failed: %v", u.id, err)
		}
	}

	// у bob известен таймзона-offset, у остальных — дефолтный 0
	if err := s.SetUTCOffset(ctx, 2, 14*3600); err != nil {
		t.Fatalf("SetUTCOffset() failed: %v", err)
	}

	users, err := s.AllUsers(ctx)
	if err != nil {
		t.Fatalf("AllUsers() failed: %v", err)
	}
	if len(users) != 3 {
		t.Fatalf("AllUsers() returned %d users, want 3", len(users))
	}
	if users[0].UserID != 1 || users[1].UserID != 2 || users[2].UserID != 3 {
		t.Errorf("users order = %+v, want [1 2 3]", users)
	}
	if users[1].UTCOffset != 14*3600 {
		t.Errorf("users[1].UTCOffset = %d, want %d", users[1].UTCOffset, 14*3600)
	}
	if users[0].UTCOffset != 0 || users[2].UTCOffset != 0 {
		t.Errorf("default UTCOffset = %d/%d, want 0/0", users[0].UTCOffset, users[2].UTCOffset)
	}
}

func TestAllUsers_Empty(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	users, err := s.AllUsers(ctx)
	if err != nil {
		t.Errorf("AllUsers() error = %v, want nil", err)
	}
	if len(users) != 0 {
		t.Errorf("AllUsers() = %+v, want empty slice", users)
	}
}

func TestSetUTCOffset(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	if err := s.RegisterUser(ctx, 1, 42, "user1"); err != nil {
		t.Fatalf("RegisterUser() failed: %v", err)
	}

	if err := s.SetUTCOffset(ctx, 1, 9*3600); err != nil {
		t.Fatalf("SetUTCOffset() failed: %v", err)
	}

	users, err := s.AllUsers(ctx)
	if err != nil {
		t.Fatalf("AllUsers() failed: %v", err)
	}
	if len(users) != 1 || users[0].UTCOffset != 9*3600 {
		t.Errorf("AllUsers() = %+v, want UTCOffset %d", users, 9*3600)
	}
}

// seedPressures наполняет БД фиксированным набором записей для тестов
// фильтрации: двое пользователей с user_id + одна legacy-строка без user_id.
func seedPressures(t *testing.T, s *Storage) {
	t.Helper()
	ctx := context.Background()

	for _, p := range []*storage.Pressure{
		{Date: "2026-01-01", DayPart: "утро", Systolic: "120", Diastolic: "80", HeartRate: "70", UserID: 1, UserName: "alice"},
		{Date: "2026-01-02", DayPart: "день", Systolic: "125", Diastolic: "82", HeartRate: "72", UserID: 1, UserName: "alice"},
		{Date: "2026-01-02", DayPart: "утро", Systolic: "130", Diastolic: "85", HeartRate: "75", UserID: 2, UserName: "bob"},
		{Date: "2026-01-03", DayPart: "вечер", Systolic: "135", Diastolic: "88", HeartRate: "78", UserID: 2, UserName: "bob"},
	} {
		if _, err := s.Save(ctx, p); err != nil {
			t.Fatalf("Save(%v) failed: %v", p, err)
		}
	}
	if err := insertLegacy(ctx, s, "2026-01-05", "утро", "140", "90", "80", "carol"); err != nil {
		t.Fatalf("insertLegacy failed: %v", err)
	}
}

// pressure returns a storage.Pressure without repetitive field listing.
func pressure(date, dayPart, sys, dia, hr string, userID int64, userName string) storage.Pressure {
	return storage.Pressure{
		Date: date, DayPart: dayPart, Systolic: sys, Diastolic: dia, HeartRate: hr,
		UserID: userID, UserName: userName,
	}
}

func pressuresEqual(got, want []storage.Pressure) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func int64Ptr(v int64) *int64    { return &v }
func stringPtr(v string) *string { return &v }

func TestExportAll(t *testing.T) {
	cases := []struct {
		name   string
		filter storage.Filter
		want   []storage.Pressure
	}{
		{
			name:   "без фильтра",
			filter: storage.Filter{},
			// legacy-строка (user_id NULL) сортируется первой: NULL < целых
			want: []storage.Pressure{
				pressure("2026-01-05", "утро", "140", "90", "80", 0, "carol"),
				pressure("2026-01-01", "утро", "120", "80", "70", 1, "alice"),
				pressure("2026-01-02", "день", "125", "82", "72", 1, "alice"),
				pressure("2026-01-02", "утро", "130", "85", "75", 2, "bob"),
				pressure("2026-01-03", "вечер", "135", "88", "78", 2, "bob"),
			},
		},
		{
			name:   "по user_id",
			filter: storage.Filter{UserID: int64Ptr(1)},
			want: []storage.Pressure{
				pressure("2026-01-01", "утро", "120", "80", "70", 1, "alice"),
				pressure("2026-01-02", "день", "125", "82", "72", 1, "alice"),
			},
		},
		{
			name:   "по user_name",
			filter: storage.Filter{UserName: stringPtr("bob")},
			want: []storage.Pressure{
				pressure("2026-01-02", "утро", "130", "85", "75", 2, "bob"),
				pressure("2026-01-03", "вечер", "135", "88", "78", 2, "bob"),
			},
		},
		{
			name:   "по user_name находит legacy-строку",
			filter: storage.Filter{UserName: stringPtr("carol")},
			want: []storage.Pressure{
				pressure("2026-01-05", "утро", "140", "90", "80", 0, "carol"),
			},
		},
		{
			name:   "по диапазону дат (включительно)",
			filter: storage.Filter{From: stringPtr("2026-01-02"), To: stringPtr("2026-01-03")},
			want: []storage.Pressure{
				pressure("2026-01-02", "день", "125", "82", "72", 1, "alice"),
				pressure("2026-01-02", "утро", "130", "85", "75", 2, "bob"),
				pressure("2026-01-03", "вечер", "135", "88", "78", 2, "bob"),
			},
		},
		{
			name:   "комбинированный: user_id + диапазон",
			filter: storage.Filter{UserID: int64Ptr(1), From: stringPtr("2026-01-01"), To: stringPtr("2026-01-01")},
			want: []storage.Pressure{
				pressure("2026-01-01", "утро", "120", "80", "70", 1, "alice"),
			},
		},
		{
			name:   "пустой результат",
			filter: storage.Filter{UserID: int64Ptr(999)},
			want:   nil,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := newTestStorage(t)
			seedPressures(t, s)

			got, err := s.ExportAll(context.Background(), c.filter)
			if err != nil {
				t.Fatalf("ExportAll() failed: %v", err)
			}
			if !pressuresEqual(got, c.want) {
				t.Errorf("ExportAll() = %+v, want %+v", got, c.want)
			}
		})
	}
}

func TestDelete(t *testing.T) {
	cases := []struct {
		name   string
		filter storage.Filter
		want   int64
	}{
		{"без фильтра (все записи)", storage.Filter{}, 5},
		{"по user_id", storage.Filter{UserID: int64Ptr(1)}, 2},
		{"по user_name", storage.Filter{UserName: stringPtr("bob")}, 2},
		{"по user_name находит legacy-строку", storage.Filter{UserName: stringPtr("carol")}, 1},
		{"по диапазону дат", storage.Filter{From: stringPtr("2026-01-02"), To: stringPtr("2026-01-03")}, 3},
		{"комбинированный", storage.Filter{UserID: int64Ptr(2), From: stringPtr("2026-01-01"), To: stringPtr("2026-01-02")}, 1},
		{"пустой результат", storage.Filter{UserID: int64Ptr(999)}, 0},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := newTestStorage(t)
			seedPressures(t, s)

			got, err := s.Delete(context.Background(), c.filter)
			if err != nil {
				t.Fatalf("Delete() failed: %v", err)
			}
			if got != c.want {
				t.Errorf("Delete() = %d, want %d", got, c.want)
			}

			// после удаления выборка без фильтра не содержит удалённых записей
			left, err := s.ExportAll(context.Background(), storage.Filter{})
			if err != nil {
				t.Fatalf("ExportAll() after Delete() failed: %v", err)
			}
			if int64(len(left)) != 5-c.want {
				t.Errorf("rows left = %d, want %d", len(left), 5-c.want)
			}
		})
	}
}

func TestMigrations_Idempotent(t *testing.T) {
	s := newTestStorage(t) // первый Init внутри
	ctx := context.Background()

	if err := s.Init(ctx); err != nil {
		t.Fatalf("second Init() failed: %v", err)
	}

	version, err := s.schemaVersion(ctx)
	if err != nil {
		t.Fatalf("schemaVersion() failed: %v", err)
	}
	if version != len(migrations) {
		t.Errorf("schema version = %d, want %d", version, len(migrations))
	}
}

func TestMigrations_FromLegacySchema(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "legacy.db")

	s, err := New(path)
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}
	t.Cleanup(func() { _ = s.db.Close() })

	// эмуляция боевой БД: старая схема, user_version = 0
	legacyDDL := `CREATE TABLE blood_pressure (date TEXT, day_part TEXT, systolic TEXT, diastolic TEXT, heart_rate TEXT, user_name TEXT)`
	if _, err := s.db.ExecContext(ctx, legacyDDL); err != nil {
		t.Fatalf("create legacy table failed: %v", err)
	}
	if err := insertLegacy(ctx, s, today(t), "утро", "120", "80", "70", "user1"); err != nil {
		t.Fatalf("insertLegacy failed: %v", err)
	}

	if err := s.Init(ctx); err != nil {
		t.Fatalf("Init() on legacy schema failed: %v", err)
	}

	// колонка user_id появилась
	if !hasColumn(ctx, t, s, "blood_pressure", "user_id") {
		t.Error("column user_id not created")
	}
	// индексы созданы
	if !hasIndex(ctx, t, s, "idx_pressure_key") {
		t.Error("index idx_pressure_key not created")
	}
	if !hasIndex(ctx, t, s, "idx_pressure_legacy") {
		t.Error("index idx_pressure_legacy not created")
	}

	// legacy-строка сохранилась и переносится по user_name
	if err := s.ClaimLegacy(ctx, 1, "user1"); err != nil {
		t.Fatalf("ClaimLegacy() failed: %v", err)
	}
	res, err := s.Show(ctx, 1, today(t))
	if err != nil {
		t.Fatalf("Show() failed: %v", err)
	}
	if len(res) != 1 {
		t.Errorf("Show() = %+v, want single migrated record", res)
	}

	// migration4 добавила utc_offset в users
	if !hasColumn(ctx, t, s, "users", "utc_offset") {
		t.Error("column utc_offset not created in users")
	}
}

func hasColumn(ctx context.Context, t *testing.T, s *Storage, table, column string) bool {
	t.Helper()
	rows, err := s.db.QueryContext(ctx, "PRAGMA table_info("+table+")")
	if err != nil {
		t.Fatalf("PRAGMA table_info failed: %v", err)
	}
	defer rows.Close()

	for rows.Next() {
		var (
			cid, notnull, pk int
			name, ctype      string
			dfltValue        any
		)
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dfltValue, &pk); err != nil {
			t.Fatalf("scan table_info failed: %v", err)
		}
		if name == column {
			return true
		}
	}

	return false
}

func hasIndex(ctx context.Context, t *testing.T, s *Storage, index string) bool {
	t.Helper()
	var name string
	err := s.db.QueryRowContext(ctx, "SELECT name FROM sqlite_master WHERE type = 'index' AND name = ?", index).Scan(&name)
	if err != nil {
		return false
	}

	return name == index
}

func TestBackupTo(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	p := &storage.Pressure{
		Date:      today(t),
		DayPart:   "утро",
		Systolic:  "120",
		Diastolic: "80",
		HeartRate: "70",
		UserID:    1,
		UserName:  "user1",
	}
	if _, err := s.Save(ctx, p); err != nil {
		t.Fatalf("Save() failed: %v", err)
	}

	backupPath := filepath.Join(t.TempDir(), "backup.db")
	if err := s.BackupTo(ctx, backupPath); err != nil {
		t.Fatalf("BackupTo() failed: %v", err)
	}

	b, err := New(backupPath)
	if err != nil {
		t.Fatalf("New(backup) failed: %v", err)
	}
	t.Cleanup(func() { _ = b.Close() })

	res, err := b.Show(ctx, 1, today(t))
	if err != nil {
		t.Fatalf("Show(backup) failed: %v", err)
	}
	if len(res) != 1 {
		t.Fatalf("Show(backup) = %+v, want 1 record", res)
	}
	if res[0].Systolic != p.Systolic {
		t.Errorf("backup record systolic = %q, want %q", res[0].Systolic, p.Systolic)
	}

	// оригинал не тронут
	orig, err := s.Show(ctx, 1, today(t))
	if err != nil {
		t.Fatalf("Show(original) failed: %v", err)
	}
	if len(orig) != 1 {
		t.Errorf("Show(original) = %+v, want 1 record", orig)
	}
}

func TestBackupTo_ExistingTarget(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	backupPath := filepath.Join(t.TempDir(), "backup.db")
	if err := os.WriteFile(backupPath, []byte("not empty"), 0o644); err != nil {
		t.Fatalf("WriteFile() failed: %v", err)
	}

	if err := s.BackupTo(ctx, backupPath); err == nil {
		t.Error("BackupTo() on existing non-empty file = nil, want error")
	}
}

func TestGetOffset_DefaultZero(t *testing.T) {
	s := newTestStorage(t)

	offset, err := s.GetOffset(context.Background())
	if err != nil {
		t.Fatalf("GetOffset() failed: %v", err)
	}
	if offset != 0 {
		t.Errorf("GetOffset() = %d, want 0 (no saved offset)", offset)
	}
}

func TestSetOffset_GetOffset_RoundTrip(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	for _, want := range []int{42, 0, 7} {
		if err := s.SetOffset(ctx, want); err != nil {
			t.Fatalf("SetOffset(%d) failed: %v", want, err)
		}
		got, err := s.GetOffset(ctx)
		if err != nil {
			t.Fatalf("GetOffset() failed: %v", err)
		}
		if got != want {
			t.Errorf("GetOffset() = %d, want %d", got, want)
		}
	}
}
