package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
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
	getUpdatesMethod   = "getUpdates"
	sendMessageMethod  = "sendMessage"
	sendDocumentMethod = "sendDocument"
	getChatMethod      = "getChat"

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
	return c.sendMessage(ctx, chatID, text, "")
}

// GetChat запрашивает информацию о чате (личный чат). utc_offset в ответе —
// смещение таймзоны пользователя в секундах от UTC; 0 = неизвестно.
func (c *Client) GetChat(ctx context.Context, chatID int) (info *ChatFullInfo, err error) {
	defer func() { err = e.WrapIfErr("can't get chat", err) }()

	q := url.Values{}
	q.Add("chat_id", strconv.Itoa(chatID))

	data, err := c.doRequest(ctx, getChatMethod, q)
	if err != nil {
		return nil, err
	}

	var res GetChatResponse

	if err := json.Unmarshal(data, &res); err != nil {
		return nil, err
	}

	if !res.Ok {
		return nil, res.toError()
	}

	return res.Result, nil
}

// SendKeyboard отправляет сообщение с кастомной reply-клавиатурой. При
// oneTime=true клавиатура скрывается после первого нажатия.
func (c *Client) SendKeyboard(ctx context.Context, chatID int, text string, keyboard [][]string, oneTime bool) (err error) {
	defer func() { err = e.WrapIfErr("can't send keyboard", err) }()

	markup := ReplyKeyboardMarkup{
		Keyboard:        keyboard,
		ResizeKeyboard:  true,
		OneTimeKeyboard: oneTime,
	}

	encoded, err := json.Marshal(markup)
	if err != nil {
		return err
	}

	return c.sendMessage(ctx, chatID, text, string(encoded))
}

// RemoveKeyboard отправляет сообщение и скрывает reply-клавиатуру.
func (c *Client) RemoveKeyboard(ctx context.Context, chatID int, text string) (err error) {
	defer func() { err = e.WrapIfErr("can't remove keyboard", err) }()

	encoded, err := json.Marshal(ReplyKeyboardRemove{RemoveKeyboard: true})
	if err != nil {
		return err
	}

	return c.sendMessage(ctx, chatID, text, string(encoded))
}

// sendMessage — общий строитель запроса sendMessage с опциональным reply_markup.
func (c *Client) sendMessage(ctx context.Context, chatID int, text string, replyMarkup string) (err error) {
	defer func() { err = e.WrapIfErr("can't send message", err) }()

	q := url.Values{}
	q.Add("chat_id", strconv.Itoa(chatID))
	q.Add("text", text)
	if replyMarkup != "" {
		q.Add("reply_markup", replyMarkup)
	}

	if _, err := c.doRequest(ctx, sendMessageMethod, q); err != nil {
		return err
	}

	return nil
}

func (c *Client) doRequest(ctx context.Context, method string, query url.Values) (data []byte, err error) {
	defer func() { err = e.WrapIfErr("can't do request", err) }()

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

	return c.do(ctx, req)
}

func (c *Client) SendDocument(ctx context.Context, chatID int, filename string, data []byte) (err error) {
	defer func() { err = e.WrapIfErr("can't send document", err) }()

	body, contentType, err := multipartBody(chatID, filename, data)
	if err != nil {
		return err
	}

	u := url.URL{
		Scheme: "https",
		Host:   c.host,
		Path:   path.Join(c.basePath, sendDocumentMethod),
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), body)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", contentType)

	respBody, err := c.do(ctx, req)
	if err != nil {
		return err
	}

	return checkOK(respBody)
}

// multipartBody собирает тело multipart/form-data: поле chat_id и файл document.
func multipartBody(chatID int, filename string, data []byte) (io.Reader, string, error) {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)

	if err := w.WriteField("chat_id", strconv.Itoa(chatID)); err != nil {
		return nil, "", err
	}

	part, err := w.CreateFormFile("document", filename)
	if err != nil {
		return nil, "", err
	}

	if _, err := part.Write(data); err != nil {
		return nil, "", err
	}

	if err := w.Close(); err != nil {
		return nil, "", err
	}

	return &buf, w.FormDataContentType(), nil
}

// do выполняет HTTP-запрос, читает тело и проверяет HTTP-статус. Все ошибки
// проходят через sanitize, чтобы токен не утёк наружу.
func (c *Client) do(ctx context.Context, req *http.Request) (data []byte, err error) {
	defer func() {
		if err != nil {
			err = c.sanitize(err)
		}
	}()

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

// checkOK проверяет поле ok ответа Telegram API и возвращает APIError при ok:false.
func checkOK(data []byte) error {
	var res UpdatesResponse
	if err := json.Unmarshal(data, &res); err != nil {
		return err
	}

	if !res.Ok {
		return res.toError()
	}

	return nil
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
