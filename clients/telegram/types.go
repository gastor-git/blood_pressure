package telegram

import "fmt"

type UpdatesResponse struct {
	Ok          bool                `json:"ok"`
	ErrorCode   int                 `json:"error_code"`
	Description string              `json:"description"`
	Parameters  *ResponseParameters `json:"parameters"`
	Result      []Update            `json:"result"`
}

// ResponseParameters содержит служебные параметры ответа Telegram API,
// например время до повторного запроса при 429.
type ResponseParameters struct {
	RetryAfter int `json:"retry_after"`
}

// APIError — типизированная ошибка Telegram API (ok:false или не-200 статус).
// Токен в неё не попадает.
type APIError struct {
	Code        int
	Description string
	retryAfter  int
}

// RetryAfter возвращает рекомендованную Telegram паузу перед повтором
// (секунды), 0 — пауза не требуется.
func (e *APIError) RetryAfter() int {
	return e.retryAfter
}

func (e *APIError) Error() string {
	if e.retryAfter > 0 {
		return fmt.Sprintf("telegram api error %d: %s (retry after %ds)", e.Code, e.Description, e.retryAfter)
	}

	return fmt.Sprintf("telegram api error %d: %s", e.Code, e.Description)
}

// toError строит APIError из полей ответа.
func (r UpdatesResponse) toError() *APIError {
	apiErr := &APIError{
		Code:        r.ErrorCode,
		Description: r.Description,
	}
	if r.Parameters != nil {
		apiErr.retryAfter = r.Parameters.RetryAfter
	}

	return apiErr
}

type GetChatResponse struct {
	Ok          bool                `json:"ok"`
	ErrorCode   int                 `json:"error_code"`
	Description string              `json:"description"`
	Parameters  *ResponseParameters `json:"parameters"`
	Result      *ChatFullInfo       `json:"result"`
}

func (r GetChatResponse) toError() *APIError {
	apiErr := &APIError{
		Code:        r.ErrorCode,
		Description: r.Description,
	}
	if r.Parameters != nil {
		apiErr.retryAfter = r.Parameters.RetryAfter
	}

	return apiErr
}

// ChatFullInfo — информация о чате из getChat. utc_offset — смещение часового
// пояса пользователя в секундах от UTC (0 = неизвестно, только для личных чатов).
type ChatFullInfo struct {
	ID        int64 `json:"id"`
	UTCOffset int   `json:"utc_offset"`
}

type Update struct {
	ID      int              `json:"update_id"`
	Message *IncomingMessage `json:"message"`
}

type IncomingMessage struct {
	Text string `json:"text"`
	From From   `json:"from"`
	Chat Chat   `json:"chat"`
}

type From struct {
	ID       int64  `json:"id"`
	Username string `json:"username"`
}

type Chat struct {
	ID int `json:"id"`
}

// ReplyKeyboardMarkup — кастомная reply-клавиатура (reply_markup в sendMessage).
type ReplyKeyboardMarkup struct {
	Keyboard        [][]string `json:"keyboard"`
	ResizeKeyboard  bool       `json:"resize_keyboard"`
	OneTimeKeyboard bool       `json:"one_time_keyboard"`
}

// ReplyKeyboardRemove — скрытие reply-клавиатуры.
type ReplyKeyboardRemove struct {
	RemoveKeyboard bool `json:"remove_keyboard"`
}
