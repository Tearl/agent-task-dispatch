package outbox

import (
	"context"
	"errors"
	"testing"
	"time"
)

type repositoryStub struct {
	messages  []Message
	completed []string
	retries   []retryRecord
}

type retryRecord struct {
	id   string
	code string
	dead bool
	at   time.Time
}

func (repository *repositoryStub) Claim(context.Context, string, []string, int, time.Duration) ([]Message, error) {
	result := repository.messages
	repository.messages = nil
	return result, nil
}
func (repository *repositoryStub) Complete(_ context.Context, _, id string) error {
	repository.completed = append(repository.completed, id)
	return nil
}
func (repository *repositoryStub) Retry(_ context.Context, _, id, code string, at time.Time, dead bool) error {
	repository.retries = append(repository.retries, retryRecord{id: id, code: code, dead: dead, at: at})
	return nil
}

func TestWorkerCompletesSuccessfulMessageAndRetriesStableFailure(t *testing.T) {
	repository := &repositoryStub{messages: []Message{{ID: "success", Topic: "execution", Attempts: 1}, {ID: "retry", Topic: "execution", Attempts: 2}}}
	calls := 0
	worker, err := NewWorker(repository, map[string]Handler{"execution": HandlerFunc(func(_ context.Context, message Message) error {
		calls++
		if message.ID == "retry" {
			return NewFailure("agent_unavailable", false)
		}
		return nil
	})}, testConfig())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)
	worker.now = func() time.Time { return now }
	processed, err := worker.RunOnce(context.Background())
	if err != nil || processed != 2 || calls != 2 {
		t.Fatalf("processed=%d calls=%d err=%v", processed, calls, err)
	}
	if len(repository.completed) != 1 || repository.completed[0] != "success" {
		t.Fatalf("unexpected completed messages: %#v", repository.completed)
	}
	if len(repository.retries) != 1 || repository.retries[0].code != "agent_unavailable" || repository.retries[0].dead || !repository.retries[0].at.Equal(now.Add(2*time.Second)) {
		t.Fatalf("unexpected retry: %#v", repository.retries)
	}
}

func TestWorkerDeadLettersUnknownPermanentAndExhaustedMessages(t *testing.T) {
	repository := &repositoryStub{messages: []Message{
		{ID: "unknown", Topic: "unknown", Attempts: 1},
		{ID: "permanent", Topic: "execution", Attempts: 1},
		{ID: "exhausted", Topic: "execution", Attempts: 5},
	}}
	worker, err := NewWorker(repository, map[string]Handler{"execution": HandlerFunc(func(_ context.Context, message Message) error {
		if message.ID == "permanent" {
			return NewFailure("invalid_payload", true)
		}
		return errors.New("transport included untrusted detail")
	})}, testConfig())
	if err != nil {
		t.Fatal(err)
	}
	if processed, runErr := worker.RunOnce(context.Background()); runErr != nil || processed != 3 {
		t.Fatalf("processed=%d err=%v", processed, runErr)
	}
	if len(repository.retries) != 3 {
		t.Fatalf("unexpected retries: %#v", repository.retries)
	}
	want := map[string]string{"unknown": "unknown_topic", "permanent": "invalid_payload", "exhausted": "handler_failed"}
	for _, retry := range repository.retries {
		if !retry.dead || retry.code != want[retry.id] {
			t.Fatalf("unexpected dead letter: %#v", retry)
		}
	}
}

func testConfig() Config {
	return Config{WorkerID: "worker-1", BatchSize: 10, Lease: time.Minute, PollEvery: time.Second, BaseBackoff: time.Second, MaxBackoff: time.Minute, MaxAttempts: 5}
}
