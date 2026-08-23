package outbox

import (
	"context"
	"errors"
	"sort"
	"strings"
	"time"
)

type Config struct {
	WorkerID    string
	BatchSize   int
	Lease       time.Duration
	PollEvery   time.Duration
	BaseBackoff time.Duration
	MaxBackoff  time.Duration
	MaxAttempts int
}

type Worker struct {
	repository Repository
	handlers   map[string]Handler
	topics     []string
	config     Config
	now        func() time.Time
}

func NewWorker(repository Repository, handlers map[string]Handler, config Config) (*Worker, error) {
	if repository == nil || strings.TrimSpace(config.WorkerID) == "" || config.BatchSize < 1 || config.BatchSize > 100 || config.Lease < time.Second || config.Lease > 15*time.Minute || config.PollEvery < 100*time.Millisecond || config.PollEvery > time.Minute || config.BaseBackoff < time.Second || config.MaxBackoff < config.BaseBackoff || config.MaxBackoff > time.Hour || config.MaxAttempts < 1 || config.MaxAttempts > 100 {
		return nil, ErrInvalidInput
	}
	stable := make(map[string]Handler, len(handlers))
	topics := make([]string, 0, len(handlers))
	for topic, handler := range handlers {
		if strings.TrimSpace(topic) == "" || handler == nil {
			return nil, ErrInvalidInput
		}
		stable[topic] = handler
		topics = append(topics, topic)
	}
	if len(topics) == 0 {
		return nil, ErrInvalidInput
	}
	sort.Strings(topics)
	return &Worker{repository: repository, handlers: stable, topics: topics, config: config, now: func() time.Time { return time.Now().UTC() }}, nil
}

func (worker *Worker) Run(ctx context.Context) error {
	ticker := time.NewTicker(worker.config.PollEvery)
	defer ticker.Stop()
	for {
		if _, err := worker.RunOnce(ctx); err != nil && !errors.Is(err, context.Canceled) {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func (worker *Worker) RunOnce(ctx context.Context) (int, error) {
	messages, err := worker.repository.Claim(ctx, worker.config.WorkerID, worker.topics, worker.config.BatchSize, worker.config.Lease)
	if err != nil {
		return 0, err
	}
	processed := 0
	for _, message := range messages {
		if err = worker.handle(ctx, message); err != nil {
			return processed, err
		}
		processed++
	}
	return processed, nil
}

func (worker *Worker) handle(ctx context.Context, message Message) error {
	handler, exists := worker.handlers[message.Topic]
	if !exists {
		return worker.repository.Retry(ctx, worker.config.WorkerID, message.ID, "unknown_topic", worker.now(), true)
	}
	err := handler.Handle(ctx, message)
	if err == nil {
		return worker.repository.Complete(ctx, worker.config.WorkerID, message.ID)
	}
	code, permanent := failureDetails(err)
	dead := permanent || message.Attempts >= worker.config.MaxAttempts
	retryAt := worker.now()
	if !dead {
		retryAt = retryAt.Add(worker.backoff(message.Attempts))
	}
	return worker.repository.Retry(ctx, worker.config.WorkerID, message.ID, code, retryAt, dead)
}

func (worker *Worker) backoff(attempt int) time.Duration {
	exponent := max(0, min(attempt-1, 30))
	delay := worker.config.BaseBackoff
	for range exponent {
		if delay >= worker.config.MaxBackoff/2 {
			return worker.config.MaxBackoff
		}
		delay *= 2
	}
	return delay
}
