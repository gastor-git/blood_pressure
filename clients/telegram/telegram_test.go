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
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
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
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("error is not *APIError: %v", err)
	}
	if apiErr.Code != 500 {
		t.Errorf("apiErr.Code = %d, want 500", apiErr.Code)
	}
}
