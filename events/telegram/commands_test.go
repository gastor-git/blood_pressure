package telegram

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	tgClient "blood-pressure-bot/clients/telegram"
	"blood-pressure-bot/lib/timeloc"
	"blood-pressure-bot/storage"
)

// --- mocks ---

type mockClient struct {
	sentChatID int
	sentTexts  []string
	sendErr    error

	sendDocCalled bool
	sentFilename  string
	sentData      []byte
	sendDocErr    error
}

func (m *mockClient) Updates(ctx context.Context, offset, limit int) ([]tgClient.Update, error) {
	return nil, nil
}

func (m *mockClient) SendMessage(ctx context.Context, chatID int, text string) error {
	if m.sendErr != nil {
		return m.sendErr
	}
	m.sentChatID = chatID
	m.sentTexts = append(m.sentTexts, text)
	return nil
}

func (m *mockClient) SendDocument(ctx context.Context, chatID int, filename string, data []byte) error {
	m.sendDocCalled = true
	m.sentChatID = chatID
	m.sentFilename = filename
	m.sentData = data
	if m.sendDocErr != nil {
		return m.sendDocErr
	}
	return nil
}

type mockStorage struct {
	saveFunc        func(ctx context.Context, p *storage.Pressure) (bool, error)
	showFunc        func(ctx context.Context, userID int64) ([]storage.Pressure, error)
	getAllFunc      func(ctx context.Context, userID int64) ([]storage.Pressure, error)
	claimLegacyFunc func(ctx context.Context, userID int64, userName string) error
	registerUser    func(ctx context.Context, userID int64, chatID int64, userName string) error

	saveCalls          int
	getAllCalls        int
	claimLegacyCalls   int
	registerUserCalls  int
	registeredUserID   int64
	registeredChatID   int64
	registeredUserName string
	savedPressure      *storage.Pressure
}

func (m *mockStorage) Save(ctx context.Context, p *storage.Pressure) (bool, error) {
	m.saveCalls++
	m.savedPressure = p
	if m.saveFunc != nil {
		return m.saveFunc(ctx, p)
	}
	return true, nil
}

func (m *mockStorage) Show(ctx context.Context, userID int64) ([]storage.Pressure, error) {
	if m.showFunc != nil {
		return m.showFunc(ctx, userID)
	}
	return nil, nil
}

func (m *mockStorage) GetAll(ctx context.Context, userID int64) ([]storage.Pressure, error) {
	m.getAllCalls++
	if m.getAllFunc != nil {
		return m.getAllFunc(ctx, userID)
	}
	return nil, nil
}

func (m *mockStorage) ClaimLegacy(ctx context.Context, userID int64, userName string) error {
	m.claimLegacyCalls++
	if m.claimLegacyFunc != nil {
		return m.claimLegacyFunc(ctx, userID, userName)
	}
	return nil
}

func (m *mockStorage) RegisterUser(ctx context.Context, userID int64, chatID int64, userName string) error {
	m.registerUserCalls++
	m.registeredUserID = userID
	m.registeredChatID = chatID
	m.registeredUserName = userName
	if m.registerUser != nil {
		return m.registerUser(ctx, userID, chatID, userName)
	}
	return nil
}

func (m *mockStorage) UsersWithoutPressure(ctx context.Context, date, dayPart string) ([]storage.User, error) {
	return nil, nil
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
		{"999 999 999", true}, // regexp пропускает по формату, диапазоны проверяет validatePressure
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

func TestValidatePressure(t *testing.T) {
	cases := []struct {
		name    string
		sys     int
		dia     int
		hr      int
		wantErr error
	}{
		{"валидные", 120, 80, 70, nil},
		{"нижние границы", 60, 30, 30, nil},
		{"верхние границы", 260, 200, 220, nil},
		{"систолическое ниже min", 59, 30, 70, ErrSystolicOutOfRange},
		{"систолическое выше max", 261, 80, 70, ErrSystolicOutOfRange},
		{"диастолическое ниже min", 120, 29, 70, ErrDiastolicOutOfRange},
		{"диастолическое выше max", 220, 201, 70, ErrDiastolicOutOfRange},
		{"пульс ниже min", 120, 80, 29, ErrHeartRateOutOfRange},
		{"пульс выше max", 120, 80, 221, ErrHeartRateOutOfRange},
		{"sys == dia", 100, 100, 70, ErrSystolicNotGreater},
		{"sys < dia", 80, 120, 70, ErrSystolicNotGreater},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := validatePressure(c.sys, c.dia, c.hr)
			if !errors.Is(err, c.wantErr) {
				t.Errorf("validatePressure(%d, %d, %d) = %v, want %v", c.sys, c.dia, c.hr, err, c.wantErr)
			}
		})
	}
}

func TestSavePressure_Invalid(t *testing.T) {
	client := &mockClient{}
	st := &mockStorage{}
	p := New(client, st)

	if err := p.doCmd(context.Background(), "999 80 70", 42, 7, "user"); err != nil {
		t.Fatalf("doCmd returned error: %v", err)
	}

	if st.saveCalls != 0 {
		t.Errorf("Save called %d times, want 0", st.saveCalls)
	}
	if len(client.sentTexts) != 1 || client.sentTexts[0] != msgInvalidPressure {
		t.Errorf("sent %v, want [%q]", client.sentTexts, msgInvalidPressure)
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
		{at(0, 0), "утро"}, // полночь — утро (00:00–11:59)
		{at(0, 59), "утро"},
		{at(1, 0), "утро"},
		{at(11, 59), "утро"}, // верхняя граница утра
		{at(12, 0), "день"},  // 12:00 — день (12:00–17:59)
		{at(12, 59), "день"},
		{at(13, 0), "день"},
		{at(17, 59), "день"}, // верхняя граница дня
		{at(18, 0), "вечер"}, // 18:00 — вечер (18:00–23:59)
		{at(18, 59), "вечер"},
		{at(19, 0), "вечер"},
		{at(23, 59), "вечер"},
	}

	for _, c := range cases {
		if got := DayPart(c.t); got != c.want {
			t.Errorf("DayPart(%02d:%02d) = %q, want %q", c.t.Hour(), c.t.Minute(), got, c.want)
		}
	}
}

func TestRegisterUser_CalledOnCommand(t *testing.T) {
	cases := []struct {
		name string
		text string
	}{
		{"показания", "120 80 70"},
		{"команда", ShowCmd},
		{"неизвестная команда", "/foo"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			client := &mockClient{}
			st := &mockStorage{}
			p := New(client, st)

			if err := p.doCmd(context.Background(), c.text, 42, 7, "user"); err != nil {
				t.Fatalf("doCmd returned error: %v", err)
			}

			if st.registerUserCalls != 1 {
				t.Fatalf("RegisterUser called %d times, want 1", st.registerUserCalls)
			}
			if st.registeredUserID != 7 {
				t.Errorf("registeredUserID = %d, want 7", st.registeredUserID)
			}
			if st.registeredChatID != 42 {
				t.Errorf("registeredChatID = %d, want 42", st.registeredChatID)
			}
			if st.registeredUserName != "user" {
				t.Errorf("registeredUserName = %q, want %q", st.registeredUserName, "user")
			}
		})
	}
}

func TestRegisterUser_StorageError(t *testing.T) {
	sentinel := errors.New("boom")
	st := &mockStorage{
		registerUser: func(ctx context.Context, userID int64, chatID int64, userName string) error {
			return sentinel
		},
	}
	p := New(&mockClient{}, st)

	err := p.doCmd(context.Background(), HelpCmd, 42, 7, "user")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, sentinel) {
		t.Errorf("error chain does not contain sentinel: %v", err)
	}
	if !strings.Contains(err.Error(), "не удалось сохранить пользователя") {
		t.Errorf("error missing wrap prefix: %v", err)
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
		{"download", DownloadCmd, msgNoSavedPressure},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			client := &mockClient{}
			p := New(client, &mockStorage{})

			if err := p.doCmd(context.Background(), c.text, 42, 7, "user"); err != nil {
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
		saveFunc: func(ctx context.Context, p *storage.Pressure) (bool, error) {
			return true, nil
		},
	}
	p := New(client, st)

	if err := p.doCmd(context.Background(), "120 80 70", 42, 7, "user"); err != nil {
		t.Fatalf("doCmd returned error: %v", err)
	}

	if st.saveCalls != 1 {
		t.Fatalf("Save called %d times, want 1", st.saveCalls)
	}

	saved := st.savedPressure
	if saved.Systolic != "120" || saved.Diastolic != "80" || saved.HeartRate != "70" {
		t.Errorf("saved pressures = %s/%s/%s, want 120/80/70", saved.Systolic, saved.Diastolic, saved.HeartRate)
	}
	if saved.UserID != 7 {
		t.Errorf("saved userID = %d, want 7", saved.UserID)
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
		saveFunc: func(ctx context.Context, p *storage.Pressure) (bool, error) {
			return false, nil
		},
	}
	p := New(client, st)

	if err := p.doCmd(context.Background(), "120 80 70", 42, 7, "user"); err != nil {
		t.Fatalf("doCmd returned error: %v", err)
	}

	if st.saveCalls != 1 {
		t.Errorf("Save called %d times, want 1", st.saveCalls)
	}
	if len(client.sentTexts) != 1 || client.sentTexts[0] != msgAlreadyExists {
		t.Errorf("sent %v, want [%q]", client.sentTexts, msgAlreadyExists)
	}
}

func TestSavePressure_StorageError(t *testing.T) {
	sentinel := errors.New("boom")
	st := &mockStorage{
		saveFunc: func(ctx context.Context, p *storage.Pressure) (bool, error) {
			return false, sentinel
		},
	}
	p := New(&mockClient{}, st)

	err := p.doCmd(context.Background(), "120 80 70", 42, 7, "user")
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
		showFunc: func(ctx context.Context, userID int64) ([]storage.Pressure, error) {
			return nil, nil
		},
	}
	p := New(client, st)

	if err := p.doCmd(context.Background(), ShowCmd, 42, 7, "user"); err != nil {
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
		showFunc: func(ctx context.Context, userID int64) ([]storage.Pressure, error) {
			return []storage.Pressure{
				{
					Date:      "2026-01-02",
					DayPart:   "утро",
					Systolic:  "120",
					Diastolic: "80",
					HeartRate: "70",
				},
			}, nil
		},
	}
	p := New(client, st)

	if err := p.doCmd(context.Background(), ShowCmd, 42, 7, "user"); err != nil {
		t.Fatalf("doCmd returned error: %v", err)
	}

	if len(client.sentTexts) != 1 || client.sentTexts[0] != data {
		t.Errorf("sent %v, want [%q]", client.sentTexts, data)
	}
}

func TestFormatPressures(t *testing.T) {
	cases := []struct {
		name string
		in   []storage.Pressure
		want string
	}{
		{
			name: "одна запись",
			in: []storage.Pressure{
				{Date: "2026-01-02", DayPart: "утро", Systolic: "120", Diastolic: "80", HeartRate: "70"},
			},
			want: "Дата: 2026-01-02, часть суток: утро, показания: 120/80/70\n\n",
		},
		{
			name: "несколько записей",
			in: []storage.Pressure{
				{Date: "2026-01-02", DayPart: "утро", Systolic: "120", Diastolic: "80", HeartRate: "70"},
				{Date: "2026-01-02", DayPart: "вечер", Systolic: "130", Diastolic: "85", HeartRate: "75"},
			},
			want: "Дата: 2026-01-02, часть суток: утро, показания: 120/80/70\n\n" +
				"Дата: 2026-01-02, часть суток: вечер, показания: 130/85/75\n\n",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := formatPressures(c.in); got != c.want {
				t.Errorf("formatPressures() = %q, want %q", got, c.want)
			}
		})
	}
}

func TestDownload_Empty(t *testing.T) {
	client := &mockClient{}
	st := &mockStorage{
		getAllFunc: func(ctx context.Context, userID int64) ([]storage.Pressure, error) {
			return nil, nil
		},
	}
	p := New(client, st)

	if err := p.doCmd(context.Background(), DownloadCmd, 42, 7, "user"); err != nil {
		t.Fatalf("doCmd returned error: %v", err)
	}

	if len(client.sentTexts) != 1 || client.sentTexts[0] != msgNoSavedPressure {
		t.Errorf("sent %v, want [%q]", client.sentTexts, msgNoSavedPressure)
	}
	if client.sendDocCalled {
		t.Error("SendDocument called, want not")
	}
}

func TestDownload_WithData(t *testing.T) {
	client := &mockClient{}
	st := &mockStorage{
		getAllFunc: func(ctx context.Context, userID int64) ([]storage.Pressure, error) {
			return []storage.Pressure{
				{Date: "2026-01-02", DayPart: "утро", Systolic: "120", Diastolic: "80", HeartRate: "70"},
				{Date: "2026-01-02", DayPart: "вечер", Systolic: "130", Diastolic: "85", HeartRate: "75"},
				{Date: "2026-01-03", DayPart: "день", Systolic: "140", Diastolic: "90", HeartRate: "80"},
			}, nil
		},
	}
	p := New(client, st)

	if err := p.doCmd(context.Background(), DownloadCmd, 42, 7, "user"); err != nil {
		t.Fatalf("doCmd returned error: %v", err)
	}

	if !client.sendDocCalled {
		t.Fatal("SendDocument not called")
	}
	if client.sentChatID != 42 {
		t.Errorf("chatID = %d, want 42", client.sentChatID)
	}
	wantName := "user_" + timeloc.Now().Format(timeloc.DateFormat) + ".csv"
	if client.sentFilename != wantName {
		t.Errorf("sentFilename = %q, want %q", client.sentFilename, wantName)
	}

	want := "\xEF\xBB\xBF" + msgCSVHeader + "\r\n" +
		"02.01.2026;120;80;70;;;;130;85;75\r\n" +
		"03.01.2026;;;;140;90;80;;;\r\n"
	if string(client.sentData) != want {
		t.Errorf("CSV = %q, want %q", client.sentData, want)
	}
}

func TestDownload_EmptyUsername(t *testing.T) {
	client := &mockClient{}
	st := &mockStorage{
		getAllFunc: func(ctx context.Context, userID int64) ([]storage.Pressure, error) {
			return []storage.Pressure{
				{Date: "2026-01-02", DayPart: "утро", Systolic: "120", Diastolic: "80", HeartRate: "70"},
			}, nil
		},
	}
	p := New(client, st)

	if err := p.doCmd(context.Background(), DownloadCmd, 42, 7, ""); err != nil {
		t.Fatalf("doCmd returned error: %v", err)
	}

	if !client.sendDocCalled {
		t.Fatal("SendDocument not called")
	}
	if !strings.HasPrefix(client.sentFilename, "user_") {
		t.Errorf("sentFilename = %q, want prefix user_", client.sentFilename)
	}
}

func TestDownload_StorageError(t *testing.T) {
	sentinel := errors.New("boom")
	client := &mockClient{}
	st := &mockStorage{
		getAllFunc: func(ctx context.Context, userID int64) ([]storage.Pressure, error) {
			return nil, sentinel
		},
	}
	p := New(client, st)

	err := p.doCmd(context.Background(), DownloadCmd, 42, 7, "user")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, sentinel) {
		t.Errorf("error chain does not contain sentinel: %v", err)
	}
	if !strings.Contains(err.Error(), "Ошибка при выполнении команды: download") {
		t.Errorf("error missing wrap prefix: %v", err)
	}
	if len(client.sentTexts) != 1 || client.sentTexts[0] != msgError {
		t.Errorf("sent %v, want [%q]", client.sentTexts, msgError)
	}
	if client.sendDocCalled {
		t.Error("SendDocument called, want not")
	}
}

func TestFormatCSV(t *testing.T) {
	cases := []struct {
		name string
		in   []storage.Pressure
		want string
	}{
		{
			name: "одна дата, три части суток",
			in: []storage.Pressure{
				{Date: "2026-01-02", DayPart: "утро", Systolic: "120", Diastolic: "80", HeartRate: "70"},
				{Date: "2026-01-02", DayPart: "день", Systolic: "125", Diastolic: "82", HeartRate: "72"},
				{Date: "2026-01-02", DayPart: "вечер", Systolic: "130", Diastolic: "85", HeartRate: "75"},
			},
			want: "\xEF\xBB\xBF" + msgCSVHeader + "\r\n" +
				"02.01.2026;120;80;70;125;82;72;130;85;75\r\n",
		},
		{
			name: "частичная — пустые ячейки",
			in: []storage.Pressure{
				{Date: "2026-01-02", DayPart: "утро", Systolic: "120", Diastolic: "80", HeartRate: "70"},
			},
			want: "\xEF\xBB\xBF" + msgCSVHeader + "\r\n" +
				"02.01.2026;120;80;70;;;;;;\r\n",
		},
		{
			name: "порядок дат и частей суток",
			in: []storage.Pressure{
				{Date: "2026-01-03", DayPart: "вечер", Systolic: "130", Diastolic: "85", HeartRate: "75"},
				{Date: "2026-01-02", DayPart: "вечер", Systolic: "135", Diastolic: "88", HeartRate: "78"},
				{Date: "2026-01-02", DayPart: "утро", Systolic: "120", Diastolic: "80", HeartRate: "70"},
				{Date: "2026-01-02", DayPart: "день", Systolic: "125", Diastolic: "82", HeartRate: "72"},
			},
			want: "\xEF\xBB\xBF" + msgCSVHeader + "\r\n" +
				"02.01.2026;120;80;70;125;82;72;135;88;78\r\n" +
				"03.01.2026;;;;;;;130;85;75\r\n",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := formatCSV(c.in); got != c.want {
				t.Errorf("formatCSV() = %q, want %q", got, c.want)
			}
		})
	}
}

func TestCSVFilename(t *testing.T) {
	today := timeloc.Now().Format(timeloc.DateFormat)

	if got := csvFilename("alice"); got != "alice_"+today+".csv" {
		t.Errorf("csvFilename(alice) = %q, want %q", got, "alice_"+today+".csv")
	}
	if got := csvFilename(""); got != "user_"+today+".csv" {
		t.Errorf("csvFilename(\"\") = %q, want %q", got, "user_"+today+".csv")
	}
}

func TestClaimLegacy_CalledOncePerUser(t *testing.T) {
	st := &mockStorage{}
	p := New(&mockClient{}, st)
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		if err := p.doCmd(ctx, HelpCmd, 42, 7, "user"); err != nil {
			t.Fatalf("doCmd returned error: %v", err)
		}
	}
	// другой пользователь — отдельный вызов
	if err := p.doCmd(ctx, HelpCmd, 42, 8, "other"); err != nil {
		t.Fatalf("doCmd returned error: %v", err)
	}
	// пустой username — backfill не вызывается
	if err := p.doCmd(ctx, HelpCmd, 42, 9, ""); err != nil {
		t.Fatalf("doCmd returned error: %v", err)
	}

	if st.claimLegacyCalls != 2 {
		t.Errorf("ClaimLegacy called %d times, want 2", st.claimLegacyCalls)
	}
}
