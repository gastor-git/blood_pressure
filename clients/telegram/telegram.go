package telegram

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"

	"blood-pressure-bot/lib/e"
)

type Client struct {
	host     string
	basePath string
	token    string
	client   http.Client
}

const (
	getUpdatesMethod  = "getUpdates"
	sendMessageMethod = "sendMessage"

	// longPollTimeout — время удержания соединения getUpdates на стороне Telegram.
	longPollTimeout = 25
	// httpTimeout — таймаут HTTP-клиента; больше longPollTimeout на чтение тела.
	httpTimeout = 30 * time.Second
	// maxResponseBytes ограничивает размер читаемого тела ответа.
	maxResponseBytes = 10 << 20
)

func New(host string, token string) *Client {
	return &Client{
		host:     host,
		basePath: newBasePath(token),
		token:    token,
		client:   http.Client{Timeout: httpTimeout},
	}
}

func newBasePath(token string) string {
	return "bot" + token
}

func (c *Client) Updates(ctx context.Context, offset int, limit int) (updates []Update, err error) {
	defer func() { err = e.WrapIfErr("can't get updates", err) }()

	q := url.Values{}
	q.Add("offset", strconv.Itoa(offset))
	q.Add("limit", strconv.Itoa(limit))
	q.Add("timeout", strconv.Itoa(longPollTimeout))

	data, err := c.doRequest(ctx, getUpdatesMethod, q)
	if err != nil {
		return nil, err
	}

	var res UpdatesResponse

	if err := json.Unmarshal(data, &res); err != nil {
		return nil, err
	}

	if !res.Ok {
		return nil, res.toError()
	}

	return res.Result, nil
}

func (c *Client) SendMessage(ctx context.Context, chatID int, text string) error {
	q := url.Values{}
	q.Add("chat_id", strconv.Itoa(chatID))
	q.Add("text", text)

	if _, err := c.doRequest(ctx, sendMessageMethod, q); err != nil {
		return e.Wrap("can't send message", err)
	}

	return nil
}

func (c *Client) doRequest(ctx context.Context, method string, query url.Values) (data []byte, err error) {
	defer func() {
		if err != nil {
			err = e.Wrap("can't do request", c.sanitize(err))
		}
	}()

	u := url.URL{
		Scheme: "https",
		Host:   c.host,
		Path:   path.Join(c.basePath, method),
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}

	req.URL.RawQuery = query.Encode()

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, statusError(resp.StatusCode, body)
	}

	return body, nil
}

// statusError разбирает тело ошибочного ответа в APIError; токена в теле нет.
func statusError(code int, body []byte) error {
	var res UpdatesResponse
	if err := json.Unmarshal(body, &res); err == nil && res.Description != "" {
		return res.toError()
	}

	return fmt.Errorf("unexpected status code %d", code)
}

// sanitize вырезает токен из текста ошибки (например, из *url.Error),
// сохраняя цепочку через Unwrap для errors.Is/As.
func (c *Client) sanitize(err error) error {
	if err == nil {
		return nil
	}

	msg := err.Error()
	if c.token != "" {
		msg = strings.ReplaceAll(msg, c.token, "***")
	}
	if msg == err.Error() {
		return err
	}

	return &sanitizedError{msg: msg, err: err}
}

type sanitizedError struct {
	msg string
	err error
}

func (e *sanitizedError) Error() string { return e.msg }
func (e *sanitizedError) Unwrap() error { return e.err }
