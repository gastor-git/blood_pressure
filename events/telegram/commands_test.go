package telegram

import (
	"context"
	"errors"
	"fmt"
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

	sentKeyboardText     string
	sentKeyboards        [][]string
	keyboardOneTime      []bool
	removeKeyboardCalled bool
	removeKeyboardText   string
	removeErr            error

	getChatCalled int
	getChatInfo   *tgClient.ChatFullInfo
	getChatErr    error
}

func (m *mockClient) Updates(ctx context.Context, offset, limit int) ([]tgClient.Update, error) {
	return nil, nil
}

// GetChat по умолчанию возвращает UTCOffset 0 (неизвестно → fallback на
// серверную таймзону), поэтому старые тесты не ломаются.
func (m *mockClient) GetChat(ctx context.Context, chatID int) (*tgClient.ChatFullInfo, error) {
	m.getChatCalled++
	if m.getChatErr != nil {
		return nil, m.getChatErr
	}
	if m.getChatInfo != nil {
		return m.getChatInfo, nil
	}

	return &tgClient.ChatFullInfo{ID: int64(chatID)}, nil
}

func (m *mockClient) SendMessage(ctx context.Context, chatID int, text string) error {
	if m.sendErr != nil {
		return m.sendErr
	}
	m.sentChatID = chatID
	m.sentTexts = append(m.sentTexts, text)
	return nil
}

func (m *mockClient) SendKeyboard(ctx context.Context, chatID int, text string, keyboard [][]string, oneTime bool) error {
	if m.sendErr != nil {
		return m.sendErr
	}
	m.sentChatID = chatID
	m.sentKeyboardText = text
	m.sentKeyboards = append(m.sentKeyboards, keyboard...)
	m.keyboardOneTime = append(m.keyboardOneTime, oneTime)
	return nil
}

func (m *mockClient) RemoveKeyboard(ctx context.Context, chatID int, text string) error {
	if m.removeErr != nil {
		return m.removeErr
	}
	m.removeKeyboardCalled = true
	m.sentChatID = chatID
	m.removeKeyboardText = text
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
	getFunc         func(ctx context.Context, userID int64, date, dayPart string) (*storage.Pressure, error)
	updateFunc      func(ctx context.Context, p *storage.Pressure) error
	showFunc        func(ctx context.Context, userID int64, date string) ([]storage.Pressure, error)
	getAllFunc      func(ctx context.Context, userID int64) ([]storage.Pressure, error)
	claimLegacyFunc func(ctx context.Context, userID int64, userName string) error
	registerUser    func(ctx context.Context, userID int64, chatID int64, userName string) error

	saveCalls          int
	getCalls           int
	updateCalls        int
	getAllCalls        int
	claimLegacyCalls   int
	registerUserCalls  int
	setUTCOffsetCalls  int
	registeredUserID   int64
	registeredChatID   int64
	registeredUserName string
	savedPressure      *storage.Pressure
	updatedPressure    *storage.Pressure
	setUTCOffsetUserID int64
	setUTCOffset       int
}

func (m *mockStorage) Save(ctx context.Context, p *storage.Pressure) (bool, error) {
	m.saveCalls++
	m.savedPressure = p
	if m.saveFunc != nil {
		return m.saveFunc(ctx, p)
	}
	return true, nil
}

func (m *mockStorage) Get(ctx context.Context, userID int64, date, dayPart string) (*storage.Pressure, error) {
	m.getCalls++
	if m.getFunc != nil {
		return m.getFunc(ctx, userID, date, dayPart)
	}
	return nil, nil
}

func (m *mockStorage) Update(ctx context.Context, p *storage.Pressure) error {
	m.updateCalls++
	m.updatedPressure = p
	if m.updateFunc != nil {
		return m.updateFunc(ctx, p)
	}
	return nil
}

func (m *mockStorage) Show(ctx context.Context, userID int64, date string) ([]storage.Pressure, error) {
	if m.showFunc != nil {
		return m.showFunc(ctx, userID, date)
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

func (m *mockStorage) SetUTCOffset(ctx context.Context, userID int64, offset int) error {
	m.setUTCOffsetCalls++
	m.setUTCOffsetUserID = userID
	m.setUTCOffset = offset
	return nil
}

func (m *mockStorage) AllUsers(ctx context.Context) ([]storage.User, error) {
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
		getFunc: func(ctx context.Context, userID int64, date, dayPart string) (*storage.Pressure, error) {
			return &storage.Pressure{Date: date, DayPart: dayPart, Systolic: "100", Diastolic: "70", HeartRate: "60"}, nil
		},
	}
	p := New(client, st)

	if err := p.doCmd(context.Background(), "120 80 70", 42, 7, "user"); err != nil {
		t.Fatalf("doCmd returned error: %v", err)
	}

	if st.saveCalls != 1 {
		t.Errorf("Save called %d times, want 1", st.saveCalls)
	}
	if st.getCalls != 1 {
		t.Errorf("Get called %d times, want 1", st.getCalls)
	}

	today := timeloc.Today()
	dayPart := DayPart(timeloc.Now())
	want := fmt.Sprintf(msgDuplicatePrompt, formatCSVDate(today), dayPart, "100", "70", "60", "120", "80", "70")
	if client.sentKeyboardText != want {
		t.Errorf("keyboard text = %q, want %q", client.sentKeyboardText, want)
	}
	if !reflect.DeepEqual(client.sentKeyboards, overwriteKeyboard) {
		t.Errorf("keyboard = %v, want %v", client.sentKeyboards, overwriteKeyboard)
	}

	s := p.sessions[7]
	if s == nil || s.state != stateConfirmOverwrite {
		t.Fatalf("session state = %v, want stateConfirmOverwrite", s)
	}
	if s.pendingSys != "120" || s.pendingDia != "80" || s.pendingHr != "70" {
		t.Errorf("pending = %s/%s/%s, want 120/80/70", s.pendingSys, s.pendingDia, s.pendingHr)
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
		showFunc: func(ctx context.Context, userID int64, date string) ([]storage.Pressure, error) {
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
	cases := []struct {
		name string
		show []storage.Pressure
		want string
	}{
		{
			name: "одна запись",
			show: []storage.Pressure{
				{Date: "2026-01-02", DayPart: "утро", Systolic: "120", Diastolic: "80", HeartRate: "70"},
			},
			want: "Дата: 02.01.2026, часть суток: утро, показания: 120/80/70\n\n",
		},
		{
			name: "сортировка по части суток утро→день→вечер",
			show: []storage.Pressure{
				{Date: "2026-01-02", DayPart: "вечер", Systolic: "130", Diastolic: "85", HeartRate: "75"},
				{Date: "2026-01-02", DayPart: "утро", Systolic: "120", Diastolic: "80", HeartRate: "70"},
				{Date: "2026-01-02", DayPart: "день", Systolic: "125", Diastolic: "82", HeartRate: "72"},
			},
			want: "Дата: 02.01.2026, часть суток: утро, показания: 120/80/70\n\n" +
				"Дата: 02.01.2026, часть суток: день, показания: 125/82/72\n\n" +
				"Дата: 02.01.2026, часть суток: вечер, показания: 130/85/75\n\n",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			client := &mockClient{}
			st := &mockStorage{
				showFunc: func(ctx context.Context, userID int64, date string) ([]storage.Pressure, error) {
					return c.show, nil
				},
			}
			p := New(client, st)

			if err := p.doCmd(context.Background(), ShowCmd, 42, 7, "user"); err != nil {
				t.Fatalf("doCmd returned error: %v", err)
			}

			if len(client.sentTexts) != 1 || client.sentTexts[0] != c.want {
				t.Errorf("sent %v, want [%q]", client.sentTexts, c.want)
			}
		})
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
			want: "Дата: 02.01.2026, часть суток: утро, показания: 120/80/70\n\n",
		},
		{
			name: "несколько записей",
			in: []storage.Pressure{
				{Date: "2026-01-02", DayPart: "утро", Systolic: "120", Diastolic: "80", HeartRate: "70"},
				{Date: "2026-01-02", DayPart: "вечер", Systolic: "130", Diastolic: "85", HeartRate: "75"},
			},
			want: "Дата: 02.01.2026, часть суток: утро, показания: 120/80/70\n\n" +
				"Дата: 02.01.2026, часть суток: вечер, показания: 130/85/75\n\n",
		},
		{
			name: "сортировка по части суток утро→день→вечер",
			in: []storage.Pressure{
				{Date: "2026-01-02", DayPart: "вечер", Systolic: "130", Diastolic: "85", HeartRate: "75"},
				{Date: "2026-01-02", DayPart: "утро", Systolic: "120", Diastolic: "80", HeartRate: "70"},
				{Date: "2026-01-02", DayPart: "день", Systolic: "125", Diastolic: "82", HeartRate: "72"},
			},
			want: "Дата: 02.01.2026, часть суток: утро, показания: 120/80/70\n\n" +
				"Дата: 02.01.2026, часть суток: день, показания: 125/82/72\n\n" +
				"Дата: 02.01.2026, часть суток: вечер, показания: 130/85/75\n\n",
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
		"02.01.2026;120/80/70;;130/85/75\r\n" +
		"03.01.2026;;140/90/80;\r\n"
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
				"02.01.2026;120/80/70;125/82/72;130/85/75\r\n",
		},
		{
			name: "частичная — пустые ячейки",
			in: []storage.Pressure{
				{Date: "2026-01-02", DayPart: "утро", Systolic: "120", Diastolic: "80", HeartRate: "70"},
			},
			want: "\xEF\xBB\xBF" + msgCSVHeader + "\r\n" +
				"02.01.2026;120/80/70;;\r\n",
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
				"02.01.2026;120/80/70;125/82/72;135/88/78\r\n" +
				"03.01.2026;;;130/85/75\r\n",
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

// --- диалог /add ---

// completeDialogSteps проходит полный диалог с указанной датой, частью суток
// и показаниями, вводимыми одним сообщением вида «120 80 70».
func completeDialogSteps(t *testing.T, p *Processor, date, dayPart, pressure string) error {
	t.Helper()

	if err := p.doCmd(context.Background(), AddCmd, 42, 7, "user"); err != nil {
		return err
	}
	if date != "" {
		if err := p.doCmd(context.Background(), date, 42, 7, "user"); err != nil {
			return err
		}
	} else if err := p.doCmd(context.Background(), msgTodayButton, 42, 7, "user"); err != nil {
		return err
	}
	if err := p.doCmd(context.Background(), dayPart, 42, 7, "user"); err != nil {
		return err
	}
	if err := p.doCmd(context.Background(), pressure, 42, 7, "user"); err != nil {
		return err
	}

	return nil
}

func TestAddDialog_FullFlow(t *testing.T) {
	client := &mockClient{}
	st := &mockStorage{
		saveFunc: func(ctx context.Context, p *storage.Pressure) (bool, error) {
			return true, nil
		},
	}
	p := New(client, st)

	if err := completeDialogSteps(t, p, "", msgMorningButton, "120 80 70"); err != nil {
		t.Fatalf("dialog failed: %v", err)
	}

	if st.saveCalls != 1 {
		t.Fatalf("Save called %d times, want 1", st.saveCalls)
	}
	saved := st.savedPressure
	if saved.Systolic != "120" || saved.Diastolic != "80" || saved.HeartRate != "70" {
		t.Errorf("saved = %s/%s/%s, want 120/80/70", saved.Systolic, saved.Diastolic, saved.HeartRate)
	}
	if saved.Date != timeloc.Today() {
		t.Errorf("date = %q, want %q", saved.Date, timeloc.Today())
	}
	if saved.DayPart != dayPartMorning {
		t.Errorf("dayPart = %q, want %q", saved.DayPart, dayPartMorning)
	}
	if saved.UserID != 7 || saved.UserName != "user" {
		t.Errorf("saved user = %d/%q, want 7/user", saved.UserID, saved.UserName)
	}
	if !client.removeKeyboardCalled || client.removeKeyboardText != msgSaved {
		t.Errorf("RemoveKeyboard = %v/%q, want true/%q", client.removeKeyboardCalled, client.removeKeyboardText, msgSaved)
	}
	if _, ok := p.sessions[7]; ok {
		t.Error("session not removed after save")
	}
}

func TestAddDialog_TypedDate(t *testing.T) {
	client := &mockClient{}
	st := &mockStorage{
		saveFunc: func(ctx context.Context, p *storage.Pressure) (bool, error) {
			return true, nil
		},
	}
	p := New(client, st)

	if err := completeDialogSteps(t, p, "01.01.2026", msgEveningButton, "120 80 70"); err != nil {
		t.Fatalf("dialog failed: %v", err)
	}

	if st.savedPressure.Date != "2026-01-01" {
		t.Errorf("date = %q, want 2026-01-01", st.savedPressure.Date)
	}
	if st.savedPressure.DayPart != dayPartEvening {
		t.Errorf("dayPart = %q, want %q", st.savedPressure.DayPart, dayPartEvening)
	}
}

func TestAddDialog_InvalidDate(t *testing.T) {
	client := &mockClient{}
	p := New(client, &mockStorage{})
	ctx := context.Background()

	if err := p.doCmd(ctx, AddCmd, 42, 7, "user"); err != nil {
		t.Fatalf("doCmd returned error: %v", err)
	}
	if err := p.doCmd(ctx, "32.13.2026", 42, 7, "user"); err != nil {
		t.Fatalf("doCmd returned error: %v", err)
	}

	if len(client.sentTexts) != 1 || client.sentTexts[0] != msgInvalidDate {
		t.Errorf("sent %v, want [%q]", client.sentTexts, msgInvalidDate)
	}
	if s := p.sessions[7]; s == nil || s.state != stateDate {
		t.Errorf("session state = %v, want stateDate", s)
	}
}

func TestAddDialog_FutureDate(t *testing.T) {
	client := &mockClient{}
	p := New(client, &mockStorage{})
	ctx := context.Background()

	if err := p.doCmd(ctx, AddCmd, 42, 7, "user"); err != nil {
		t.Fatalf("doCmd returned error: %v", err)
	}
	future := timeloc.Now().AddDate(1, 0, 0).Format(timeloc.UserDateFormat)
	if err := p.doCmd(ctx, future, 42, 7, "user"); err != nil {
		t.Fatalf("doCmd returned error: %v", err)
	}

	if len(client.sentTexts) != 1 || client.sentTexts[0] != msgFutureDate {
		t.Errorf("sent %v, want [%q]", client.sentTexts, msgFutureDate)
	}
	if s := p.sessions[7]; s == nil || s.state != stateDate {
		t.Errorf("session state = %v, want stateDate", s)
	}
}

func TestAddDialog_InvalidDayPart(t *testing.T) {
	client := &mockClient{}
	p := New(client, &mockStorage{})
	ctx := context.Background()

	if err := p.doCmd(ctx, AddCmd, 42, 7, "user"); err != nil {
		t.Fatalf("doCmd returned error: %v", err)
	}
	if err := p.doCmd(ctx, msgTodayButton, 42, 7, "user"); err != nil {
		t.Fatalf("doCmd returned error: %v", err)
	}
	if err := p.doCmd(ctx, "ночь", 42, 7, "user"); err != nil {
		t.Fatalf("doCmd returned error: %v", err)
	}

	if len(client.sentTexts) != 1 || client.sentTexts[0] != msgInvalidDayPart {
		t.Errorf("sent %v, want [%q]", client.sentTexts, msgInvalidDayPart)
	}
	if s := p.sessions[7]; s == nil || s.state != stateDayPart {
		t.Errorf("session state = %v, want stateDayPart", s)
	}
}

func TestAddDialog_ValueOutOfRange(t *testing.T) {
	client := &mockClient{}
	p := New(client, &mockStorage{})
	ctx := context.Background()

	if err := completeDialogSteps(t, p, "", msgMorningButton, "500 80 70"); err != nil {
		t.Fatalf("dialog failed: %v", err)
	}

	if n := len(client.sentTexts); n != 2 || client.sentTexts[n-1] != msgInvalidPressure {
		t.Errorf("sent %v, want last [%q]", client.sentTexts, msgInvalidPressure)
	}
	if s := p.sessions[7]; s == nil || s.state != statePressure {
		t.Errorf("session state = %v, want statePressure", s)
	}

	// корректные показания продолжают диалог
	if err := p.doCmd(ctx, "120 80 70", 42, 7, "user"); err != nil {
		t.Fatalf("doCmd returned error: %v", err)
	}
	if _, ok := p.sessions[7]; ok {
		t.Error("session not removed after valid pressure")
	}
}

func TestAddDialog_SysNotGreater(t *testing.T) {
	client := &mockClient{}
	p := New(client, &mockStorage{})

	if err := completeDialogSteps(t, p, "", msgMorningButton, "100 150 70"); err != nil {
		t.Fatalf("dialog failed: %v", err)
	}

	if n := len(client.sentTexts); n != 2 || client.sentTexts[n-1] != msgInvalidPressure {
		t.Errorf("sent %v, want last [%q]", client.sentTexts, msgInvalidPressure)
	}
	if s := p.sessions[7]; s == nil || s.state != statePressure {
		t.Errorf("session state = %v, want statePressure", s)
	}
}

func TestAddDialog_InvalidFormat(t *testing.T) {
	cases := []struct {
		name string
		in   string
	}{
		{"не число", "abc"},
		{"два числа", "120 80"},
		{"четыре числа", "120 80 70 60"},
		{"пусто", ""},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			client := &mockClient{}
			p := New(client, &mockStorage{})

			if err := completeDialogSteps(t, p, "", msgMorningButton, c.in); err != nil {
				t.Fatalf("dialog failed: %v", err)
			}

			if n := len(client.sentTexts); n != 2 || client.sentTexts[n-1] != msgInvalidPressureFormat {
				t.Errorf("sent %v, want last [%q]", client.sentTexts, msgInvalidPressureFormat)
			}
			if s := p.sessions[7]; s == nil || s.state != statePressure {
				t.Errorf("session state = %v, want statePressure", s)
			}
		})
	}
}

func TestAddDialog_Cancel(t *testing.T) {
	client := &mockClient{}
	p := New(client, &mockStorage{})
	ctx := context.Background()

	if err := p.doCmd(ctx, AddCmd, 42, 7, "user"); err != nil {
		t.Fatalf("doCmd returned error: %v", err)
	}
	if err := p.doCmd(ctx, CancelCmd, 42, 7, "user"); err != nil {
		t.Fatalf("doCmd returned error: %v", err)
	}

	if len(client.sentTexts) != 1 || client.sentTexts[0] != msgCancel {
		t.Errorf("sent %v, want [%q]", client.sentTexts, msgCancel)
	}
	if _, ok := p.sessions[7]; ok {
		t.Error("session not removed on cancel")
	}
}

func TestAddDialog_CommandCancelsSession(t *testing.T) {
	client := &mockClient{}
	st := &mockStorage{
		showFunc: func(ctx context.Context, userID int64, date string) ([]storage.Pressure, error) {
			return nil, nil
		},
	}
	p := New(client, st)
	ctx := context.Background()

	if err := p.doCmd(ctx, AddCmd, 42, 7, "user"); err != nil {
		t.Fatalf("doCmd returned error: %v", err)
	}
	if err := p.doCmd(ctx, ShowCmd, 42, 7, "user"); err != nil {
		t.Fatalf("doCmd returned error: %v", err)
	}

	if _, ok := p.sessions[7]; ok {
		t.Error("session not removed on command")
	}
	if len(client.sentTexts) != 1 || client.sentTexts[0] != msgNoSavedPressure {
		t.Errorf("sent %v, want [%q]", client.sentTexts, msgNoSavedPressure)
	}
}

func TestAddDialog_Duplicate(t *testing.T) {
	client := &mockClient{}
	st := &mockStorage{
		saveFunc: func(ctx context.Context, p *storage.Pressure) (bool, error) {
			return false, nil
		},
		getFunc: func(ctx context.Context, userID int64, date, dayPart string) (*storage.Pressure, error) {
			return &storage.Pressure{Date: date, DayPart: dayPart, Systolic: "100", Diastolic: "70", HeartRate: "60"}, nil
		},
	}
	p := New(client, st)

	if err := completeDialogSteps(t, p, "", msgMorningButton, "120 80 70"); err != nil {
		t.Fatalf("dialog failed: %v", err)
	}

	if client.removeKeyboardCalled {
		t.Error("RemoveKeyboard called, want not (duplicate asks for confirmation)")
	}

	want := fmt.Sprintf(msgDuplicatePrompt, formatCSVDate(timeloc.Today()), dayPartMorning, "100", "70", "60", "120", "80", "70")
	if client.sentKeyboardText != want {
		t.Errorf("keyboard text = %q, want %q", client.sentKeyboardText, want)
	}

	// сессия не отменяется, а переходит к подтверждению перезаписи
	if s := p.sessions[7]; s == nil || s.state != stateConfirmOverwrite {
		t.Errorf("session state = %v, want stateConfirmOverwrite", s)
	}
}

func TestAddDialog_StorageError(t *testing.T) {
	sentinel := errors.New("boom")
	client := &mockClient{}
	st := &mockStorage{
		saveFunc: func(ctx context.Context, p *storage.Pressure) (bool, error) {
			return false, sentinel
		},
	}
	p := New(client, st)

	err := completeDialogSteps(t, p, "", msgMorningButton, "120 80 70")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, sentinel) {
		t.Errorf("error chain does not contain sentinel: %v", err)
	}
	if !strings.Contains(err.Error(), "Ошибка при сохранении показаний давления") {
		t.Errorf("error missing wrap prefix: %v", err)
	}
	if n := len(client.sentTexts); n != 2 || client.sentTexts[n-1] != msgError {
		t.Errorf("sent %v, want last [%q]", client.sentTexts, msgError)
	}
	if _, ok := p.sessions[7]; ok {
		t.Error("session not removed on storage error")
	}
}

// --- подтверждение перезаписи дубликата ---

// duplicateStorage возвращает mockStorage, который при сохранении сообщает о
// дубликате, а Get возвращает существующую запись.
func duplicateStorage() *mockStorage {
	return &mockStorage{
		saveFunc: func(ctx context.Context, p *storage.Pressure) (bool, error) {
			return false, nil
		},
		getFunc: func(ctx context.Context, userID int64, date, dayPart string) (*storage.Pressure, error) {
			return &storage.Pressure{Date: date, DayPart: dayPart, Systolic: "100", Diastolic: "70", HeartRate: "60"}, nil
		},
	}
}

func TestOverwrite_Confirm_Quick(t *testing.T) {
	client := &mockClient{}
	st := duplicateStorage()
	p := New(client, st)
	ctx := context.Background()

	// дата и часть суток фиксируются один раз: быстрый ввод берёт их из now
	dayPart := DayPart(timeloc.Now())

	if err := p.doCmd(ctx, "120 80 70", 42, 7, "user"); err != nil {
		t.Fatalf("doCmd returned error: %v", err)
	}
	if err := p.doCmd(ctx, msgOverwriteButton, 42, 7, "user"); err != nil {
		t.Fatalf("doCmd returned error: %v", err)
	}

	if st.updateCalls != 1 {
		t.Fatalf("Update called %d times, want 1", st.updateCalls)
	}
	updated := st.updatedPressure
	if updated.Systolic != "120" || updated.Diastolic != "80" || updated.HeartRate != "70" {
		t.Errorf("updated = %s/%s/%s, want 120/80/70", updated.Systolic, updated.Diastolic, updated.HeartRate)
	}
	if updated.Date != timeloc.Today() {
		t.Errorf("updated date = %q, want %q", updated.Date, timeloc.Today())
	}
	if updated.DayPart != dayPart {
		t.Errorf("updated dayPart = %q, want %q", updated.DayPart, dayPart)
	}
	if updated.UserID != 7 || updated.UserName != "user" {
		t.Errorf("updated user = %d/%q, want 7/user", updated.UserID, updated.UserName)
	}
	if !client.removeKeyboardCalled || client.removeKeyboardText != msgOverwritten {
		t.Errorf("RemoveKeyboard = %v/%q, want true/%q", client.removeKeyboardCalled, client.removeKeyboardText, msgOverwritten)
	}
	if _, ok := p.sessions[7]; ok {
		t.Error("session not removed after overwrite")
	}
}

func TestOverwrite_Confirm_Dialog(t *testing.T) {
	client := &mockClient{}
	st := duplicateStorage()
	p := New(client, st)
	ctx := context.Background()

	if err := completeDialogSteps(t, p, "", msgMorningButton, "120 80 70"); err != nil {
		t.Fatalf("dialog failed: %v", err)
	}
	if err := p.doCmd(ctx, msgOverwriteButton, 42, 7, "user"); err != nil {
		t.Fatalf("doCmd returned error: %v", err)
	}

	if st.updateCalls != 1 {
		t.Fatalf("Update called %d times, want 1", st.updateCalls)
	}
	updated := st.updatedPressure
	if updated.Systolic != "120" || updated.Diastolic != "80" || updated.HeartRate != "70" {
		t.Errorf("updated = %s/%s/%s, want 120/80/70", updated.Systolic, updated.Diastolic, updated.HeartRate)
	}
	if updated.Date != timeloc.Today() || updated.DayPart != dayPartMorning {
		t.Errorf("updated key = %q/%q, want %q/%q", updated.Date, updated.DayPart, timeloc.Today(), dayPartMorning)
	}
	if updated.UserID != 7 || updated.UserName != "user" {
		t.Errorf("updated user = %d/%q, want 7/user", updated.UserID, updated.UserName)
	}
	if !client.removeKeyboardCalled || client.removeKeyboardText != msgOverwritten {
		t.Errorf("RemoveKeyboard = %v/%q, want true/%q", client.removeKeyboardCalled, client.removeKeyboardText, msgOverwritten)
	}
	if _, ok := p.sessions[7]; ok {
		t.Error("session not removed after overwrite")
	}
}

func TestOverwrite_KeepExisting(t *testing.T) {
	client := &mockClient{}
	st := duplicateStorage()
	p := New(client, st)
	ctx := context.Background()

	if err := p.doCmd(ctx, "120 80 70", 42, 7, "user"); err != nil {
		t.Fatalf("doCmd returned error: %v", err)
	}
	if err := p.doCmd(ctx, msgKeepButton, 42, 7, "user"); err != nil {
		t.Fatalf("doCmd returned error: %v", err)
	}

	if st.updateCalls != 0 {
		t.Errorf("Update called %d times, want 0", st.updateCalls)
	}
	if !client.removeKeyboardCalled || client.removeKeyboardText != msgKeepExisting {
		t.Errorf("RemoveKeyboard = %v/%q, want true/%q", client.removeKeyboardCalled, client.removeKeyboardText, msgKeepExisting)
	}
	if _, ok := p.sessions[7]; ok {
		t.Error("session not removed after keep existing")
	}
}

func TestOverwrite_InvalidChoice(t *testing.T) {
	client := &mockClient{}
	p := New(client, duplicateStorage())
	ctx := context.Background()

	if err := p.doCmd(ctx, "120 80 70", 42, 7, "user"); err != nil {
		t.Fatalf("doCmd returned error: %v", err)
	}
	if err := p.doCmd(ctx, "да", 42, 7, "user"); err != nil {
		t.Fatalf("doCmd returned error: %v", err)
	}

	if client.sentKeyboardText != msgInvalidOverwriteChoice {
		t.Errorf("keyboard text = %q, want %q", client.sentKeyboardText, msgInvalidOverwriteChoice)
	}
	if st := p.sessions[7]; st == nil || st.state != stateConfirmOverwrite {
		t.Errorf("session state = %v, want stateConfirmOverwrite", st)
	}

	// корректный выбор завершает подтверждение
	if err := p.doCmd(ctx, msgKeepButton, 42, 7, "user"); err != nil {
		t.Fatalf("doCmd returned error: %v", err)
	}
	if _, ok := p.sessions[7]; ok {
		t.Error("session not removed after valid choice")
	}
}

func TestOverwrite_GetError(t *testing.T) {
	sentinel := errors.New("boom")
	client := &mockClient{}
	st := &mockStorage{
		saveFunc: func(ctx context.Context, p *storage.Pressure) (bool, error) {
			return false, nil
		},
		getFunc: func(ctx context.Context, userID int64, date, dayPart string) (*storage.Pressure, error) {
			return nil, sentinel
		},
	}
	p := New(client, st)

	err := p.doCmd(context.Background(), "120 80 70", 42, 7, "user")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, sentinel) {
		t.Errorf("error chain does not contain sentinel: %v", err)
	}
	if n := len(client.sentTexts); n != 1 || client.sentTexts[n-1] != msgError {
		t.Errorf("sent %v, want last [%q]", client.sentTexts, msgError)
	}
	if _, ok := p.sessions[7]; ok {
		t.Error("session created on Get error")
	}
}

func TestOverwrite_UpdateError(t *testing.T) {
	sentinel := errors.New("boom")
	client := &mockClient{}
	st := duplicateStorage()
	st.updateFunc = func(ctx context.Context, p *storage.Pressure) error {
		return sentinel
	}
	p := New(client, st)
	ctx := context.Background()

	if err := p.doCmd(ctx, "120 80 70", 42, 7, "user"); err != nil {
		t.Fatalf("doCmd returned error: %v", err)
	}
	err := p.doCmd(ctx, msgOverwriteButton, 42, 7, "user")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, sentinel) {
		t.Errorf("error chain does not contain sentinel: %v", err)
	}
	if n := len(client.sentTexts); n != 1 || client.sentTexts[n-1] != msgError {
		t.Errorf("sent %v, want last [%q]", client.sentTexts, msgError)
	}
	if _, ok := p.sessions[7]; ok {
		t.Error("session not removed on Update error")
	}
}

func TestOverwrite_RecordGone(t *testing.T) {
	// Save сообщил о дубликате, но Get записи не нашёл (ручное вмешательство
	// в БД) — значения сохраняются напрямую.
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

	if st.saveCalls != 2 {
		t.Errorf("Save called %d times, want 2", st.saveCalls)
	}
	if len(client.sentTexts) != 1 || client.sentTexts[0] != msgSaved {
		t.Errorf("sent %v, want [%q]", client.sentTexts, msgSaved)
	}
	if _, ok := p.sessions[7]; ok {
		t.Error("session created when record is gone")
	}
}

func TestParseUserDate(t *testing.T) {
	cases := []struct {
		in      string
		want    string
		wantErr bool
	}{
		{"02.01.2026", "2026-01-02", false},
		{"31.12.2025", "2025-12-31", false},
		{"02.01.2026 ", "", true},
		{"2026-01-02", "", true},
		{"32.01.2026", "", true},
		{"02.13.2026", "", true},
		{"", "", true},
		{"abc", "", true},
	}

	for _, c := range cases {
		got, err := parseUserDate(c.in)
		if c.wantErr {
			if err == nil {
				t.Errorf("parseUserDate(%q) = %q, want error", c.in, got)
			}
			continue
		}
		if err != nil || got != c.want {
			t.Errorf("parseUserDate(%q) = %q, %v; want %q, nil", c.in, got, err, c.want)
		}
	}
}

func TestDayPartKey(t *testing.T) {
	cases := []struct {
		in   string
		want string
		ok   bool
	}{
		{"Утро", dayPartMorning, true},
		{"утро", dayPartMorning, true},
		{"День", dayPartDay, true},
		{"Вечер", dayPartEvening, true},
		{"вечер", dayPartEvening, true},
		{"ночь", "", false},
		{"", "", false},
	}

	for _, c := range cases {
		got, ok := dayPartKey(c.in)
		if ok != c.ok || got != c.want {
			t.Errorf("dayPartKey(%q) = %q, %v; want %q, %v", c.in, got, ok, c.want, c.ok)
		}
	}
}

// --- персональная таймзона (getChat.utc_offset) ---

func TestEnsureTimezone_CacheOnce(t *testing.T) {
	client := &mockClient{
		getChatInfo: &tgClient.ChatFullInfo{ID: 7, UTCOffset: 5 * 3600},
	}
	st := &mockStorage{}
	p := New(client, st)
	ctx := context.Background()

	p.ensureTimezone(ctx, 42, 7)
	p.ensureTimezone(ctx, 42, 7)
	p.ensureTimezone(ctx, 42, 7)

	if client.getChatCalled != 1 {
		t.Errorf("GetChat called %d times, want 1 (кэш)", client.getChatCalled)
	}
	if st.setUTCOffsetCalls != 1 {
		t.Errorf("SetUTCOffset called %d times, want 1", st.setUTCOffsetCalls)
	}
	if st.setUTCOffsetUserID != 7 || st.setUTCOffset != 5*3600 {
		t.Errorf("SetUTCOffset = %d/%d, want 7/%d", st.setUTCOffsetUserID, st.setUTCOffset, 5*3600)
	}
}

func TestEnsureTimezone_ZeroOffsetFallback(t *testing.T) {
	// offset 0 кэшируется, но в БД не пишется: 0 = неизвестно, fallback на
	// серверную таймзону.
	client := &mockClient{}
	st := &mockStorage{}
	p := New(client, st)
	ctx := context.Background()

	p.ensureTimezone(ctx, 42, 7)
	p.ensureTimezone(ctx, 42, 7)

	if client.getChatCalled != 1 {
		t.Errorf("GetChat called %d times, want 1 (offset 0 кэшируется)", client.getChatCalled)
	}
	if st.setUTCOffsetCalls != 0 {
		t.Errorf("SetUTCOffset called %d times, want 0", st.setUTCOffsetCalls)
	}
}

func TestEnsureTimezone_FallbackOnError(t *testing.T) {
	client := &mockClient{getChatErr: errors.New("getChat boom")}
	st := &mockStorage{}
	p := New(client, st)
	ctx := context.Background()

	p.ensureTimezone(ctx, 42, 7)
	if st.setUTCOffsetCalls != 0 {
		t.Errorf("SetUTCOffset called %d times, want 0", st.setUTCOffsetCalls)
	}

	// offset не закэширован — повторный вызов снова идёт в GetChat
	p.ensureTimezone(ctx, 42, 7)
	if client.getChatCalled != 2 {
		t.Errorf("GetChat called %d times, want 2 (при ошибке не кэшируем)", client.getChatCalled)
	}
}

func TestSavePressure_UserTimezone(t *testing.T) {
	client := &mockClient{
		getChatInfo: &tgClient.ChatFullInfo{ID: 7, UTCOffset: 14 * 3600},
	}
	st := &mockStorage{
		saveFunc: func(ctx context.Context, p *storage.Pressure) (bool, error) {
			return true, nil
		},
	}
	p := New(client, st)

	if err := p.doCmd(context.Background(), "120 80 70", 42, 7, "user"); err != nil {
		t.Fatalf("doCmd returned error: %v", err)
	}

	local := time.Now().In(time.FixedZone("", 14*3600))
	if st.savedPressure.Date != local.Format(timeloc.DateFormat) {
		t.Errorf("saved date = %q, want %q (локальная дата)", st.savedPressure.Date, local.Format(timeloc.DateFormat))
	}
	if st.savedPressure.DayPart != DayPart(local) {
		t.Errorf("saved dayPart = %q, want %q (локальная часть суток)", st.savedPressure.DayPart, DayPart(local))
	}
	if client.getChatCalled != 1 {
		t.Errorf("GetChat called %d times, want 1", client.getChatCalled)
	}
}

func TestShow_UserTimezone(t *testing.T) {
	client := &mockClient{
		getChatInfo: &tgClient.ChatFullInfo{ID: 7, UTCOffset: 14 * 3600},
	}
	var gotDate string
	st := &mockStorage{
		showFunc: func(ctx context.Context, userID int64, date string) ([]storage.Pressure, error) {
			gotDate = date
			return nil, nil
		},
	}
	p := New(client, st)

	if err := p.doCmd(context.Background(), ShowCmd, 42, 7, "user"); err != nil {
		t.Fatalf("doCmd returned error: %v", err)
	}

	want := time.Now().In(time.FixedZone("", 14*3600)).Format(timeloc.DateFormat)
	if gotDate != want {
		t.Errorf("Show date = %q, want %q (локальная дата)", gotDate, want)
	}
	if len(client.sentTexts) != 1 || client.sentTexts[0] != msgNoSavedPressure {
		t.Errorf("sent %v, want [%q]", client.sentTexts, msgNoSavedPressure)
	}
}
