package sqsconsumer

import (
	"context"
	"errors"
	"regexp"
	"time"
)

var (
	ErrInvalidInput   = errors.New("invalid SQS consumer input")
	ErrLedgerConflict = errors.New("processed message identity conflict")
)

var consumerNamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_.-]{0,127}$`)

type Consumption struct {
	ConsumerName    string
	MessageID       string
	Topic           string
	DedupeKey       string
	EnvelopeHash    string
	BrokerMessageID string
	ProcessedAt     time.Time
}

type Ledger interface {
	Lookup(context.Context, string, string) (Consumption, bool, error)
	Complete(context.Context, Consumption) (bool, error)
}

type Outcome string

const (
	OutcomeNoMessage Outcome = "no_message"
	OutcomeProcessed Outcome = "processed"
	OutcomeReplay    Outcome = "replay"
	OutcomeRetry     Outcome = "retry"
)

type Result struct {
	Outcome      Outcome
	MessageID    string
	FailureCode  string
	Permanent    bool
	ReceiveCount int
}

type Config struct {
	ConsumerName      string
	QueueURL          string
	ExpectedTopic     string
	WaitTime          time.Duration
	VisibilityTimeout time.Duration
	HeartbeatEvery    time.Duration
	APIRequestTimeout time.Duration
	BaseBackoff       time.Duration
	MaxBackoff        time.Duration
}
