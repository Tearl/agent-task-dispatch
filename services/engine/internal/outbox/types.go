package outbox

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"time"
)

var (
	ErrInvalidInput = errors.New("invalid outbox input")
	ErrLeaseLost    = errors.New("outbox message lease lost")
)

var failureCodePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_.-]{0,127}$`)

type Message struct {
	ID          string
	DedupeKey   string
	Topic       string
	Payload     json.RawMessage
	AvailableAt time.Time
	Attempts    int
	CreatedAt   time.Time
}

type Repository interface {
	Claim(context.Context, string, []string, int, time.Duration) ([]Message, error)
	Complete(context.Context, string, string) error
	Retry(context.Context, string, string, string, time.Time, bool) error
}

type Handler interface {
	Handle(context.Context, Message) error
}

type HandlerFunc func(context.Context, Message) error

func (function HandlerFunc) Handle(ctx context.Context, message Message) error {
	return function(ctx, message)
}

type Failure struct {
	Code      string
	Permanent bool
}

func (failure Failure) Error() string { return failure.Code }

func NewFailure(code string, permanent bool) error {
	if !failureCodePattern.MatchString(code) {
		return Failure{Code: "handler_failed", Permanent: permanent}
	}
	return Failure{Code: code, Permanent: permanent}
}

func failureDetails(err error) (string, bool) {
	var failure Failure
	if errors.As(err, &failure) {
		return failure.Code, failure.Permanent
	}
	return "handler_failed", false
}
