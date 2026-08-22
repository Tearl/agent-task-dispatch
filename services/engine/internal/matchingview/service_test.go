package matchingview

import (
	"context"
	"errors"
	"testing"

	"github.com/example/agent-platform/engine/internal/auth"
)

type repositoryStub struct{ calls int }

func (repo *repositoryStub) Get(context.Context, string, string) (View, error) {
	repo.calls++
	return View{Task: Task{ID: "task-1"}}, nil
}

func TestServiceEnforcesPublisherBeforeRepository(t *testing.T) {
	repo := &repositoryStub{}
	service, _ := NewService(repo)
	if _, err := service.Get(context.Background(), auth.Session{UserID: "user", Roles: []string{"agent_provider"}}, "task-1"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("expected forbidden, got %v", err)
	}
	if repo.calls != 0 {
		t.Fatal("forbidden request reached repository")
	}
	if _, err := service.Get(context.Background(), auth.Session{UserID: "user", Roles: []string{"publisher"}}, "task-1"); err != nil || repo.calls != 1 {
		t.Fatalf("publisher request failed: %v", err)
	}
}
