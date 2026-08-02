package event_consumer

import (
	"errors"
	"fmt"
	"testing"
	"time"
)

// retryErr — ошибка с рекомендацией паузы (имитация *telegram.APIError).
type retryErr struct {
	seconds int
}

func (e retryErr) Error() string { return "rate limited" }

func (e retryErr) RetryAfter() int { return e.seconds }

func TestRetryDelay(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want time.Duration
	}{
		{"обычная ошибка — базовый retry", errors.New("boom"), errRetryDelay},
		{"nil — базовый retry", nil, errRetryDelay},
		{"retry_after 3с", retryErr{seconds: 3}, 3 * time.Second},
		{"retry_after 0 — базовый retry", retryErr{seconds: 0}, errRetryDelay},
		{"обёрнутая ошибка", fmt.Errorf("wrap: %w", retryErr{seconds: 5}), 5 * time.Second},
		{"retry_after выше предела — cap", retryErr{seconds: 3600}, maxRetryDelay},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := retryDelay(c.err); got != c.want {
				t.Errorf("retryDelay() = %v, want %v", got, c.want)
			}
		})
	}
}
