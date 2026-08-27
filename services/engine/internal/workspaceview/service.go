package workspaceview

import (
	"context"
	"database/sql"
	"errors"
	"slices"
	"time"

	"github.com/example/agent-platform/engine/internal/auth"
	"github.com/lib/pq"
)

var (
	ErrForbidden = errors.New("workspace view forbidden")
	ErrInvalid   = errors.New("invalid workspace view")
)

type Task struct {
	ID               string    `json:"id"`
	Title            string    `json:"title"`
	Category         string    `json:"category"`
	Status           string    `json:"status"`
	OverviewBudget   string    `json:"overviewBudget"`
	FormalBudget     string    `json:"formalBudget"`
	ExternalCostCap  string    `json:"externalCostCap"`
	AggregateVersion int64     `json:"aggregateVersion"`
	DeletionPending  bool      `json:"deletionPending"`
	Deadline         time.Time `json:"deadline"`
	CreatedAt        time.Time `json:"createdAt"`
	UpdatedAt        time.Time `json:"updatedAt"`
}

type Agent struct {
	ID                       string     `json:"id"`
	Name                     string     `json:"name"`
	Category                 string     `json:"category"`
	Tags                     []string   `json:"tags"`
	Capabilities             string     `json:"capabilities"`
	AuthorBio                string     `json:"authorBio"`
	EndpointURL              string     `json:"endpointUrl,omitempty"`
	Status                   string     `json:"status"`
	Health                   string     `json:"health"`
	HealthCheckedAt          *time.Time `json:"healthCheckedAt,omitempty"`
	HealthValidUntil         *time.Time `json:"healthValidUntil,omitempty"`
	MaxConcurrency           int        `json:"maxConcurrency"`
	ActiveCapacity           int        `json:"activeCapacity"`
	AggregateVersion         int64      `json:"aggregateVersion"`
	EstimatedDurationSeconds int64      `json:"estimatedDurationSeconds"`
	CurrentPriceVersion      *int       `json:"currentPriceVersion,omitempty"`
	CurrentCredentialVersion *int       `json:"currentCredentialVersion,omitempty"`
	OverviewPrice            string     `json:"overviewPrice,omitempty"`
	FormalPrice              string     `json:"formalPrice,omitempty"`
	UpdatedAt                time.Time  `json:"updatedAt"`
}

type Notification struct {
	ID           string    `json:"id"`
	Action       string    `json:"action"`
	ResourceType string    `json:"resourceType"`
	ResourceID   string    `json:"resourceId"`
	OccurredAt   time.Time `json:"occurredAt"`
}

type Service struct{ db *sql.DB }

func NewService(db *sql.DB) (*Service, error) {
	if db == nil {
		return nil, ErrInvalid
	}
	return &Service{db: db}, nil
}

func (service *Service) Tasks(ctx context.Context, session auth.Session) ([]Task, error) {
	if !authorized(session, "publisher") {
		return nil, ErrForbidden
	}
	rows, err := service.db.QueryContext(ctx, `SELECT task_id,title,expert_type,status,overview_budget::text,formal_budget::text,external_cost_cap::text,aggregate_version,deletion_requested_at IS NOT NULL,deadline,created_at,updated_at FROM tasks WHERE publisher_id=$1 AND deleted_at IS NULL ORDER BY created_at DESC LIMIT 200`, session.UserID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	values := []Task{}
	for rows.Next() {
		var value Task
		if err = rows.Scan(&value.ID, &value.Title, &value.Category, &value.Status, &value.OverviewBudget, &value.FormalBudget, &value.ExternalCostCap, &value.AggregateVersion, &value.DeletionPending, &value.Deadline, &value.CreatedAt, &value.UpdatedAt); err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	return values, rows.Err()
}

func (service *Service) Agents(ctx context.Context, session auth.Session, marketplace bool) ([]Agent, error) {
	role := "agent_provider"
	where := "agent.owner_id=$1"
	arguments := []any{session.UserID}
	if marketplace {
		role = "publisher"
		where = "agent.status='active'"
		arguments = nil
	}
	if !authorized(session, role) {
		return nil, ErrForbidden
	}
	rows, err := service.db.QueryContext(ctx, `SELECT agent.agent_id,agent.name,agent.category,agent.tags,agent.capabilities,agent.author_bio,agent.endpoint_url,agent.status,agent.health,agent.health_checked_at,agent.health_valid_until,agent.max_concurrency,(SELECT count(*)::integer FROM agent_capacity_leases lease WHERE lease.agent_id=agent.agent_id AND lease.released_at IS NULL AND lease.expires_at>statement_timestamp()),agent.aggregate_version,agent.estimated_duration_seconds,agent.current_price_version,agent.current_credential_version,COALESCE(price.overview_price::text,''),COALESCE(price.formal_package_gross_price::text,''),agent.updated_at FROM agents agent LEFT JOIN agent_price_versions price ON price.agent_id=agent.agent_id AND price.version_no=agent.current_price_version WHERE `+where+` ORDER BY agent.updated_at DESC LIMIT 200`, arguments...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	values := []Agent{}
	for rows.Next() {
		var value Agent
		var tags pq.StringArray
		if err = rows.Scan(&value.ID, &value.Name, &value.Category, &tags, &value.Capabilities, &value.AuthorBio, &value.EndpointURL, &value.Status, &value.Health, &value.HealthCheckedAt, &value.HealthValidUntil, &value.MaxConcurrency, &value.ActiveCapacity, &value.AggregateVersion, &value.EstimatedDurationSeconds, &value.CurrentPriceVersion, &value.CurrentCredentialVersion, &value.OverviewPrice, &value.FormalPrice, &value.UpdatedAt); err != nil {
			return nil, err
		}
		value.Tags = []string(tags)
		if marketplace {
			value.EndpointURL = ""
			value.CurrentCredentialVersion = nil
		}
		values = append(values, value)
	}
	return values, rows.Err()
}

func (service *Service) Notifications(ctx context.Context, session auth.Session) ([]Notification, error) {
	if session.UserID == "" {
		return nil, ErrForbidden
	}
	rows, err := service.db.QueryContext(ctx, `SELECT event_id,action,resource_type,resource_id,occurred_at FROM audit_events WHERE actor_id=$1 ORDER BY occurred_at DESC,event_id DESC LIMIT 100`, session.UserID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	values := []Notification{}
	for rows.Next() {
		var value Notification
		if err = rows.Scan(&value.ID, &value.Action, &value.ResourceType, &value.ResourceID, &value.OccurredAt); err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	return values, rows.Err()
}

func authorized(session auth.Session, role string) bool {
	return session.UserID != "" && slices.Contains(session.Roles, role)
}
