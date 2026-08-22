package financeview

import (
	"context"
	"errors"
	"testing"

	"github.com/example/agent-platform/engine/internal/auth"
)

type repositoryStub struct{ publisherCalls, agentCalls, reconciliationCalls int }

func (repo *repositoryStub) Publisher(context.Context, string) (PublisherView, error) {
	repo.publisherCalls++
	return PublisherView{}, nil
}
func (repo *repositoryStub) Agent(context.Context, string) (AgentView, error) {
	repo.agentCalls++
	return AgentView{}, nil
}
func (repo *repositoryStub) Reconciliation(context.Context) (ReconciliationView, error) {
	repo.reconciliationCalls++
	return ReconciliationView{}, nil
}

func TestFinanceViewsEnforceRoleBoundaries(t *testing.T) {
	repo := &repositoryStub{}
	service, _ := NewService(repo)
	ctx := context.Background()
	if _, err := service.Publisher(ctx, auth.Session{UserID: "u", Roles: []string{"agent_provider"}}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("publisher boundary: %v", err)
	}
	if _, err := service.Agent(ctx, auth.Session{UserID: "u", Roles: []string{"publisher"}}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("agent boundary: %v", err)
	}
	if _, err := service.Reconciliation(ctx, auth.Session{UserID: "u", Roles: []string{"publisher"}}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("admin boundary: %v", err)
	}
	if repo.publisherCalls != 0 || repo.agentCalls != 0 || repo.reconciliationCalls != 0 {
		t.Fatal("forbidden view reached repository")
	}
	_, _ = service.Publisher(ctx, auth.Session{UserID: "u", Roles: []string{"publisher"}})
	_, _ = service.Agent(ctx, auth.Session{UserID: "u", Roles: []string{"agent_provider"}})
	_, _ = service.Reconciliation(ctx, auth.Session{UserID: "u", Roles: []string{"admin"}})
	if repo.publisherCalls != 1 || repo.agentCalls != 1 || repo.reconciliationCalls != 1 {
		t.Fatal("authorized view was not routed")
	}
}
