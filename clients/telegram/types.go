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
	RetryAfter  int
}

func (e *APIError) Error() string {
	if e.RetryAfter > 0 {
		return fmt.Sprintf("telegram api error %d: %s (retry after %ds)", e.Code, e.Description, e.RetryAfter)
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
		apiErr.RetryAfter = r.Parameters.RetryAfter
	}

	return apiErr
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
	Username string `json:"username"`
}

type Chat struct {
	ID int `json:"id"`
}
