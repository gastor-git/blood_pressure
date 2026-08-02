package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
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

	res, err := s.Show(ctx, 1)
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

	res, err := s.Show(ctx, 1)
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

	res1, err := s.Show(ctx, 1)
	if err != nil {
		t.Fatalf("Show(1) failed: %v", err)
	}
	if len(res1) != 1 || res1[0].Systolic != "120" {
		t.Errorf("Show(1) = %+v, want single record 120/...", res1)
	}

	res2, err := s.Show(ctx, 2)
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

	res, err := s.Show(ctx, 1)
	if err != nil {
		t.Fatalf("Show() failed: %v", err)
	}
	if len(res) != 0 {
		t.Errorf("Show() = %+v, want empty (record is from another day)", res)
	}
}

func TestShow_Empty(t *testing.T) {
	s := newTestStorage(t)

	res, err := s.Show(context.Background(), 999)
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
	res, err := s.Show(ctx, 1)
	if err != nil {
		t.Fatalf("Show() failed: %v", err)
	}
	if len(res) != 0 {
		t.Fatalf("Show() before claim = %+v, want empty", res)
	}

	if err := s.ClaimLegacy(ctx, 1, "user1"); err != nil {
		t.Fatalf("ClaimLegacy() failed: %v", err)
	}

	res, err = s.Show(ctx, 1)
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
	res, err := s.Show(ctx, 2)
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

	users, err := s.UsersWithoutPressure(ctx, today(t), "утро")
	if err != nil {
		t.Fatalf("UsersWithoutPressure() failed: %v", err)
	}
	if len(users) != 1 {
		t.Fatalf("UsersWithoutPressure() returned %d users, want 1", len(users))
	}
	got := users[0]
	if got.UserID != 1 || got.ChatID != 42 || got.UserName != "user1" {
		t.Errorf("UsersWithoutPressure() = %+v, want {1 42 user1}", got)
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

	users, err := s.UsersWithoutPressure(ctx, today(t), "утро")
	if err != nil {
		t.Fatalf("UsersWithoutPressure() failed: %v", err)
	}
	if len(users) != 1 {
		t.Fatalf("UsersWithoutPressure() returned %d users, want 1", len(users))
	}
	got := users[0]
	if got.ChatID != 43 || got.UserName != "user_renamed" {
		t.Errorf("UsersWithoutPressure() = %+v, want updated {1 43 user_renamed}", got)
	}
}

func TestUsersWithoutPressure(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()
	date := today(t)

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

	// alice уже передала показания за утро
	p := &storage.Pressure{
		Date:      date,
		DayPart:   "утро",
		Systolic:  "120",
		Diastolic: "80",
		HeartRate: "70",
		UserID:    1,
		UserName:  "alice",
	}
	if _, err := s.Save(ctx, p); err != nil {
		t.Fatalf("Save() failed: %v", err)
	}

	// legacy-строка без user_id (не принадлежит пользователю, не блокирует)
	if err := insertLegacy(ctx, s, date, "утро", "130", "85", "75", "alice"); err != nil {
		t.Fatalf("insertLegacy failed: %v", err)
	}

	users, err := s.UsersWithoutPressure(ctx, date, "утро")
	if err != nil {
		t.Fatalf("UsersWithoutPressure() failed: %v", err)
	}
	if len(users) != 2 {
		t.Fatalf("UsersWithoutPressure() returned %d users, want 2", len(users))
	}
	if users[0].UserID == 1 || users[1].UserID == 1 {
		t.Errorf("alice (передала показания) не должна попасть в выборку: %+v", users)
	}
}

func TestUsersWithoutPressure_Empty(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	users, err := s.UsersWithoutPressure(ctx, today(t), "вечер")
	if err != nil {
		t.Errorf("UsersWithoutPressure() error = %v, want nil", err)
	}
	if len(users) != 0 {
		t.Errorf("UsersWithoutPressure() = %+v, want empty slice", users)
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
	res, err := s.Show(ctx, 1)
	if err != nil {
		t.Fatalf("Show() failed: %v", err)
	}
	if len(res) != 1 {
		t.Errorf("Show() = %+v, want single migrated record", res)
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
