package cli

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"blood-pressure-bot/storage"
)

// fakeStore — реализация интерфейса store для тестов Run.
type fakeStore struct {
	exportAll  func(ctx context.Context, filter storage.Filter) ([]storage.Pressure, error)
	deleteFunc func(ctx context.Context, filter storage.Filter) (int64, error)
	backupTo   func(ctx context.Context, path string) error
	err        error

	initCalls      int
	exportAllCalls int
	deleteCalls    int
	backupCalls    int
	backupPath     string
	closeCalls     int
	lastFilter     storage.Filter
	deletedCount   int64
}

func (f *fakeStore) Init(ctx context.Context) error {
	f.initCalls++
	return f.err
}

func (f *fakeStore) ExportAll(ctx context.Context, filter storage.Filter) ([]storage.Pressure, error) {
	f.exportAllCalls++
	f.lastFilter = filter
	if f.exportAll != nil {
		return f.exportAll(ctx, filter)
	}
	return nil, nil
}

func (f *fakeStore) Delete(ctx context.Context, filter storage.Filter) (int64, error) {
	f.deleteCalls++
	f.lastFilter = filter
	if f.deleteFunc != nil {
		return f.deleteFunc(ctx, filter)
	}
	return f.deletedCount, nil
}

func (f *fakeStore) Close() error {
	f.closeCalls++
	return nil
}

func (f *fakeStore) BackupTo(ctx context.Context, path string) error {
	f.backupCalls++
	f.backupPath = path
	if f.backupTo != nil {
		return f.backupTo(ctx, path)
	}
	return f.err
}

func TestParseCLIDate(t *testing.T) {
	cases := []struct {
		in      string
		want    string
		wantErr bool
	}{
		{"02.01.2026", "2026-01-02", false},
		{"31.12.2025", "2025-12-31", false},
		{"01.01.2026", "2026-01-01", false},
		{"2026-01-02", "", true},
		{"32.01.2026", "", true},
		{"02.13.2026", "", true},
		{"02.01.26", "", true},
		{"", "", true},
		{"abc", "", true},
	}

	for _, c := range cases {
		got, err := parseCLIDate(c.in)
		if c.wantErr {
			if err == nil {
				t.Errorf("parseCLIDate(%q) = %q, want error", c.in, got)
			}
			continue
		}
		if err != nil || got != c.want {
			t.Errorf("parseCLIDate(%q) = %q, %v; want %q, nil", c.in, got, err, c.want)
		}
	}
}

func TestParseFilter(t *testing.T) {
	from := "2026-01-01"
	to := "2026-01-31"
	userID := int64(7)
	userName := "alice"

	cases := []struct {
		name     string
		userID   *int64
		userName string
		from     string
		to       string
		want     storage.Filter
		wantErr  bool
	}{
		{"без фильтров", nil, "", "", "", storage.Filter{}, false},
		{"user-id 0 — без фильтра", &userID, "", "", "", storage.Filter{UserID: &userID}, false},
		{"user-name", nil, userName, "", "", storage.Filter{UserName: &userName}, false},
		{"диапазон дат", nil, "", "01.01.2026", "31.01.2026", storage.Filter{From: &from, To: &to}, false},
		{"только from", nil, "", "01.01.2026", "", storage.Filter{From: &from}, false},
		{"только to", nil, "", "", "31.01.2026", storage.Filter{To: &to}, false},
		{"комбинированный", &userID, userName, "01.01.2026", "31.01.2026",
			storage.Filter{UserID: &userID, UserName: &userName, From: &from, To: &to}, false},
		{"from позже to", nil, "", "31.01.2026", "01.01.2026", storage.Filter{}, true},
		{"невалидная дата", nil, "", "32.01.2026", "", storage.Filter{}, true},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := parseFilter(c.userID, c.userName, c.from, c.to)
			if c.wantErr {
				if err == nil {
					t.Fatal("parseFilter() = nil, want error")
				}
				return
			}
			if err != nil {
				t.Fatalf("parseFilter() failed: %v", err)
			}
			if !filtersEqual(got, c.want) {
				t.Errorf("parseFilter() = %+v, want %+v", got, c.want)
			}
		})
	}
}

func filtersEqual(a, b storage.Filter) bool {
	return ptrEqual(a.UserID, b.UserID) &&
		ptrEqual(a.UserName, b.UserName) &&
		ptrEqual(a.From, b.From) &&
		ptrEqual(a.To, b.To)
}

func ptrEqual[T comparable](a, b *T) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

func TestFormatExportCSV(t *testing.T) {
	cases := []struct {
		name string
		in   []storage.Pressure
		want string
	}{
		{
			name: "два пользователя, пустые ячейки",
			in: []storage.Pressure{
				{Date: "2026-01-02", DayPart: "утро", Systolic: "120", Diastolic: "80", HeartRate: "70", UserID: 1, UserName: "alice"},
				{Date: "2026-01-02", DayPart: "вечер", Systolic: "130", Diastolic: "85", HeartRate: "75", UserID: 2, UserName: "bob"},
				{Date: "2026-01-03", DayPart: "день", Systolic: "140", Diastolic: "90", HeartRate: "80", UserID: 2, UserName: "bob"},
			},
			want: "\xEF\xBB\xBFUser_ID;User_name;Дата;Утро;День;Вечер\r\n" +
				"1;alice;02.01.2026;120/80/70;;\r\n" +
				"2;bob;02.01.2026;;;130/85/75\r\n" +
				"2;bob;03.01.2026;;140/90/80;\r\n",
		},
		{
			name: "legacy-строка — пустой User_ID",
			in: []storage.Pressure{
				{Date: "2026-01-02", DayPart: "утро", Systolic: "120", Diastolic: "80", HeartRate: "70", UserName: "carol"},
			},
			want: "\xEF\xBB\xBFUser_ID;User_name;Дата;Утро;День;Вечер\r\n" +
				";carol;02.01.2026;120/80/70;;\r\n",
		},
		{
			name: "пустой user_name",
			in: []storage.Pressure{
				{Date: "2026-01-02", DayPart: "утро", Systolic: "120", Diastolic: "80", HeartRate: "70", UserID: 1},
			},
			want: "\xEF\xBB\xBFUser_ID;User_name;Дата;Утро;День;Вечер\r\n" +
				"1;;02.01.2026;120/80/70;;\r\n",
		},
		{
			name: "полная строка по частям суток",
			in: []storage.Pressure{
				{Date: "2026-01-02", DayPart: "утро", Systolic: "120", Diastolic: "80", HeartRate: "70", UserID: 1, UserName: "alice"},
				{Date: "2026-01-02", DayPart: "день", Systolic: "125", Diastolic: "82", HeartRate: "72", UserID: 1, UserName: "alice"},
				{Date: "2026-01-02", DayPart: "вечер", Systolic: "130", Diastolic: "85", HeartRate: "75", UserID: 1, UserName: "alice"},
			},
			want: "\xEF\xBB\xBFUser_ID;User_name;Дата;Утро;День;Вечер\r\n" +
				"1;alice;02.01.2026;120/80/70;125/82/72;130/85/75\r\n",
		},
		{
			name: "сортировка по user_id и дате",
			in: []storage.Pressure{
				{Date: "2026-01-03", DayPart: "утро", Systolic: "135", Diastolic: "88", HeartRate: "78", UserID: 2, UserName: "bob"},
				{Date: "2026-01-01", DayPart: "утро", Systolic: "120", Diastolic: "80", HeartRate: "70", UserID: 1, UserName: "alice"},
				{Date: "2026-01-02", DayPart: "утро", Systolic: "125", Diastolic: "82", HeartRate: "72", UserID: 1, UserName: "alice"},
			},
			want: "\xEF\xBB\xBFUser_ID;User_name;Дата;Утро;День;Вечер\r\n" +
				"1;alice;01.01.2026;120/80/70;;\r\n" +
				"1;alice;02.01.2026;125/82/72;;\r\n" +
				"2;bob;03.01.2026;135/88/78;;\r\n",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := formatExportCSV(c.in); got != c.want {
				t.Errorf("formatExportCSV() = %q, want %q", got, c.want)
			}
		})
	}
}

func TestRun_Help(t *testing.T) {
	fs := &fakeStore{}

	if err := Run([]string{"help"}, fs); err != nil {
		t.Fatalf("Run(help) failed: %v", err)
	}
	if fs.exportAllCalls != 0 || fs.deleteCalls != 0 {
		t.Error("help должен вызывать только вывод справки")
	}
}

func TestRun_UnknownCommand(t *testing.T) {
	fs := &fakeStore{}

	err := Run([]string{"foo"}, fs)
	if err == nil {
		t.Fatal("Run(foo) = nil, want error")
	}
	if !errors.Is(err, ErrUnknownCommand) {
		t.Errorf("Run(foo) error = %v, want ErrUnknownCommand", err)
	}
}

func TestRun_Export(t *testing.T) {
	out := filepath.Join(t.TempDir(), "out.csv")
	fs := &fakeStore{
		exportAll: func(ctx context.Context, filter storage.Filter) ([]storage.Pressure, error) {
			return []storage.Pressure{
				{Date: "2026-01-02", DayPart: "утро", Systolic: "120", Diastolic: "80", HeartRate: "70", UserID: 1, UserName: "alice"},
			}, nil
		},
	}

	err := Run([]string{"export", "-user-id", "1", "-user-name", "alice", "-from", "01.01.2026", "-to", "31.01.2026", "-out", out}, fs)
	if err != nil {
		t.Fatalf("Run(export) failed: %v", err)
	}

	if fs.exportAllCalls != 1 {
		t.Fatalf("ExportAll called %d times, want 1", fs.exportAllCalls)
	}
	if fs.lastFilter.UserID == nil || *fs.lastFilter.UserID != 1 {
		t.Errorf("filter.UserID = %v, want 1", fs.lastFilter.UserID)
	}
	if fs.lastFilter.UserName == nil || *fs.lastFilter.UserName != "alice" {
		t.Errorf("filter.UserName = %v, want alice", fs.lastFilter.UserName)
	}
	if fs.lastFilter.From == nil || *fs.lastFilter.From != "2026-01-01" {
		t.Errorf("filter.From = %v, want 2026-01-01", fs.lastFilter.From)
	}
	if fs.lastFilter.To == nil || *fs.lastFilter.To != "2026-01-31" {
		t.Errorf("filter.To = %v, want 2026-01-31", fs.lastFilter.To)
	}

	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read out file failed: %v", err)
	}
	want := "\xEF\xBB\xBFUser_ID;User_name;Дата;Утро;День;Вечер\r\n" +
		"1;alice;02.01.2026;120/80/70;;\r\n"
	if string(data) != want {
		t.Errorf("file content = %q, want %q", data, want)
	}
}

func TestRun_Export_Empty(t *testing.T) {
	out := filepath.Join(t.TempDir(), "empty.csv")
	fs := &fakeStore{} // ExportAll вернёт nil, nil

	if err := Run([]string{"export", "-out", out}, fs); err != nil {
		t.Fatalf("Run(export) failed: %v", err)
	}

	if fs.exportAllCalls != 1 {
		t.Errorf("ExportAll called %d times, want 1", fs.exportAllCalls)
	}
	if _, err := os.Stat(out); !os.IsNotExist(err) {
		t.Errorf("file %s should not be created when no data", out)
	}
}

func TestRun_Delete_WithoutYes(t *testing.T) {
	fs := &fakeStore{}

	err := Run([]string{"delete"}, fs)
	if err == nil {
		t.Fatal("Run(delete) = nil, want refusal without --yes")
	}
	if fs.deleteCalls != 0 {
		t.Errorf("Delete called %d times, want 0 (отказ до подтверждения)", fs.deleteCalls)
	}
}

func TestRun_Delete_WithYes(t *testing.T) {
	fs := &fakeStore{
		deleteFunc: func(ctx context.Context, filter storage.Filter) (int64, error) {
			return 42, nil
		},
	}

	if err := Run([]string{"delete", "-yes"}, fs); err != nil {
		t.Fatalf("Run(delete -yes) failed: %v", err)
	}
	if fs.deleteCalls != 1 {
		t.Errorf("Delete called %d times, want 1", fs.deleteCalls)
	}
	if fs.lastFilter.UserID != nil || fs.lastFilter.UserName != nil || fs.lastFilter.From != nil || fs.lastFilter.To != nil {
		t.Errorf("filter = %+v, want empty (удаление всех записей)", fs.lastFilter)
	}
}

func TestRun_Delete_WithYesAndFilter(t *testing.T) {
	fs := &fakeStore{
		deleteFunc: func(ctx context.Context, filter storage.Filter) (int64, error) {
			return 2, nil
		},
	}

	err := Run([]string{"delete", "-user-name", "bob", "-from", "01.01.2026", "-yes"}, fs)
	if err != nil {
		t.Fatalf("Run(delete) failed: %v", err)
	}
	if fs.deleteCalls != 1 {
		t.Errorf("Delete called %d times, want 1", fs.deleteCalls)
	}
	if fs.lastFilter.UserName == nil || *fs.lastFilter.UserName != "bob" {
		t.Errorf("filter.UserName = %v, want bob", fs.lastFilter.UserName)
	}
	if fs.lastFilter.From == nil || *fs.lastFilter.From != "2026-01-01" {
		t.Errorf("filter.From = %v, want 2026-01-01", fs.lastFilter.From)
	}
}

func TestRun_Backup(t *testing.T) {
	out := filepath.Join(t.TempDir(), "backup.db")
	fs := &fakeStore{}

	if err := Run([]string{"backup", "-out", out}, fs); err != nil {
		t.Fatalf("Run(backup) failed: %v", err)
	}
	if fs.backupCalls != 1 {
		t.Errorf("BackupTo called %d times, want 1", fs.backupCalls)
	}
	if fs.backupPath != out {
		t.Errorf("BackupTo path = %q, want %q", fs.backupPath, out)
	}
}

func TestRun_Backup_DefaultName(t *testing.T) {
	fs := &fakeStore{}

	if err := Run([]string{"backup"}, fs); err != nil {
		t.Fatalf("Run(backup) failed: %v", err)
	}
	want := defaultBackupName()
	if fs.backupPath != want {
		t.Errorf("BackupTo path = %q, want default %q", fs.backupPath, want)
	}
}

func TestRun_Backup_Error(t *testing.T) {
	fs := &fakeStore{err: errors.New("boom")}

	if err := Run([]string{"backup"}, fs); err == nil {
		t.Fatal("Run(backup) = nil, want error")
	}
}

func TestRun_Health(t *testing.T) {
	fs := &fakeStore{}

	if err := Run([]string{"health"}, fs); err != nil {
		t.Fatalf("Run(health) failed: %v", err)
	}
	if fs.initCalls != 1 {
		t.Errorf("Init called %d times, want 1", fs.initCalls)
	}
}

func TestRun_Health_Error(t *testing.T) {
	fs := &fakeStore{err: errors.New("boom")}

	if err := Run([]string{"health"}, fs); err == nil {
		t.Fatal("Run(health) = nil, want error")
	}
}
