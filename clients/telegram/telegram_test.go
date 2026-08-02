package telegram

import (
	"context"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const testToken = "test-token"

// newTestClient поднимает TLS-сервер, доверяющий тестовому сертификату httptest.
func newTestClient(t *testing.T, handler http.Handler) (*Client, *httptest.Server) {
	t.Helper()

	ts := httptest.NewTLSServer(handler)
	t.Cleanup(ts.Close)

	host := strings.TrimPrefix(ts.URL, "https://")
	c := New(host, testToken)
	// транспорт httptest доверяет тестовому сертификату
	c.client.Transport = ts.Client().Transport

	return c, ts
}

func TestSendDocument(t *testing.T) {
	c, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if !strings.HasSuffix(r.URL.Path, "/"+sendDocumentMethod) {
			t.Errorf("path = %s, want suffix /%s", r.URL.Path, sendDocumentMethod)
		}

		mediaType, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if err != nil {
			t.Fatalf("parse Content-Type failed: %v", err)
		}
		if mediaType != "multipart/form-data" {
			t.Errorf("media type = %q, want multipart/form-data", mediaType)
		}

		var gotChatID, gotFilename, gotContent string
		mr := multipart.NewReader(r.Body, params["boundary"])
		for {
			part, err := mr.NextPart()
			if err == io.EOF {
				break
			}
			if err != nil {
				t.Fatalf("NextPart failed: %v", err)
			}

			b, err := io.ReadAll(part)
			if err != nil {
				t.Fatalf("read part failed: %v", err)
			}

			switch part.FormName() {
			case "chat_id":
				gotChatID = string(b)
			case "document":
				gotFilename = part.FileName()
				gotContent = string(b)
			}
		}

		if gotChatID != "42" {
			t.Errorf("chat_id = %q, want 42", gotChatID)
		}
		if gotFilename != "user_2026-01-02.csv" {
			t.Errorf("filename = %q, want user_2026-01-02.csv", gotFilename)
		}
		if gotContent != "data" {
			t.Errorf("file content = %q, want data", gotContent)
		}

		_, _ = io.WriteString(w, `{"ok":true,"result":null}`)
	}))

	if err := c.SendDocument(context.Background(), 42, "user_2026-01-02.csv", []byte("data")); err != nil {
		t.Fatalf("SendDocument() failed: %v", err)
	}
}

func TestSendDocument_OKFalse(t *testing.T) {
	c, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"ok":false,"error_code":400,"description":"bad request"}`)
	}))

	err := c.SendDocument(context.Background(), 42, "user.csv", []byte("data"))
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	apiErr, ok := errors.AsType[*APIError](err)
	if !ok {
		t.Fatalf("error is not *APIError: %v", err)
	}
	if apiErr.Code != 400 || apiErr.Description != "bad request" {
		t.Errorf("apiErr = %+v, want 400/bad request", apiErr)
	}
}

func TestSendDocument_NonOKStatus(t *testing.T) {
	c, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, `{"ok":false,"error_code":500,"description":"internal"}`)
	}))

	err := c.SendDocument(context.Background(), 42, "user.csv", []byte("data"))
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	apiErr, ok := errors.AsType[*APIError](err)
	if !ok {
		t.Fatalf("error is not *APIError: %v", err)
	}
	if apiErr.Code != 500 {
		t.Errorf("apiErr.Code = %d, want 500", apiErr.Code)
	}
}

func TestSendKeyboard(t *testing.T) {
	c, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/"+sendMessageMethod) {
			t.Errorf("path = %s, want suffix /%s", r.URL.Path, sendMessageMethod)
		}
		if got := r.URL.Query().Get("chat_id"); got != "42" {
			t.Errorf("chat_id = %q, want 42", got)
		}
		if got := r.URL.Query().Get("text"); got != "prompt" {
			t.Errorf("text = %q, want prompt", got)
		}
		want := `{"keyboard":[["7","8","9"],["4","5","6"],["1","2","3"],["0","⌫","Готово"]],"resize_keyboard":true,"one_time_keyboard":false}`
		if got := r.URL.Query().Get("reply_markup"); got != want {
			t.Errorf("reply_markup = %s, want %s", got, want)
		}

		_, _ = io.WriteString(w, `{"ok":true,"result":null}`)
	}))

	keyboard := [][]string{{"7", "8", "9"}, {"4", "5", "6"}, {"1", "2", "3"}, {"0", "⌫", "Готово"}}
	if err := c.SendKeyboard(context.Background(), 42, "prompt", keyboard, false); err != nil {
		t.Fatalf("SendKeyboard() failed: %v", err)
	}
}

func TestRemoveKeyboard(t *testing.T) {
	c, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		want := `{"remove_keyboard":true}`
		if got := r.URL.Query().Get("reply_markup"); got != want {
			t.Errorf("reply_markup = %s, want %s", got, want)
		}

		_, _ = io.WriteString(w, `{"ok":true,"result":null}`)
	}))

	if err := c.RemoveKeyboard(context.Background(), 42, "done"); err != nil {
		t.Fatalf("RemoveKeyboard() failed: %v", err)
	}
}

func TestGetChat(t *testing.T) {
	c, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/"+getChatMethod) {
			t.Errorf("path = %s, want suffix /%s", r.URL.Path, getChatMethod)
		}
		if got := r.URL.Query().Get("chat_id"); got != "42" {
			t.Errorf("chat_id = %q, want 42", got)
		}

		_, _ = io.WriteString(w, `{"ok":true,"result":{"id":42,"utc_offset":18000}}`)
	}))

	info, err := c.GetChat(context.Background(), 42)
	if err != nil {
		t.Fatalf("GetChat() failed: %v", err)
	}
	if info == nil {
		t.Fatal("GetChat() = nil, want info")
	}
	if info.ID != 42 {
		t.Errorf("info.ID = %d, want 42", info.ID)
	}
	if info.UTCOffset != 18000 {
		t.Errorf("info.UTCOffset = %d, want 18000", info.UTCOffset)
	}
}

func TestGetChat_OKFalse(t *testing.T) {
	c, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"ok":false,"error_code":403,"description":"forbidden"}`)
	}))

	info, err := c.GetChat(context.Background(), 42)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if info != nil {
		t.Errorf("GetChat() = %+v, want nil on error", info)
	}
	apiErr, ok := errors.AsType[*APIError](err)
	if !ok {
		t.Fatalf("error is not *APIError: %v", err)
	}
	if apiErr.Code != 403 || apiErr.Description != "forbidden" {
		t.Errorf("apiErr = %+v, want 403/forbidden", apiErr)
	}
}
