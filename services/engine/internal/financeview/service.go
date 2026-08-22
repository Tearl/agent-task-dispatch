package financeview

import (
	"context"
	"slices"

	"github.com/example/agent-platform/engine/internal/auth"
)

type Service struct{ repository Repository }

func NewService(repository Repository) (*Service, error) {
	if repository == nil {
		return nil, ErrInvalid
	}
	return &Service{repository: repository}, nil
}

func (service *Service) Publisher(ctx context.Context, session auth.Session) (PublisherView, error) {
	if session.UserID == "" || !slices.Contains(session.Roles, "publisher") {
		return PublisherView{}, ErrForbidden
	}
	return service.repository.Publisher(ctx, session.UserID)
}

func (service *Service) Agent(ctx context.Context, session auth.Session) (AgentView, error) {
	if session.UserID == "" || !slices.Contains(session.Roles, "agent_provider") {
		return AgentView{}, ErrForbidden
	}
	return service.repository.Agent(ctx, session.UserID)
}

func (service *Service) Reconciliation(ctx context.Context, session auth.Session) (ReconciliationView, error) {
	if session.UserID == "" || !slices.Contains(session.Roles, "admin") {
		return ReconciliationView{}, ErrForbidden
	}
	return service.repository.Reconciliation(ctx)
}
