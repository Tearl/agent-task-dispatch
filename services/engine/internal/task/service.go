package task

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math/big"
	"slices"
	"strings"
	"time"

	"github.com/example/agent-platform/engine/internal/auth"
)

var (
	ErrForbidden    = errors.New("task operation forbidden")
	ErrNotFound     = errors.New("task not found")
	ErrStaleVersion = errors.New("stale task aggregate version")
	ErrInvalidState = errors.New("invalid task state")
	ErrInvalidInput = errors.New("invalid task input")
)

const (
	StatusDraft         = "draft"
	StatusPendingEscrow = "pending_escrow"
)

type AcceptanceCriterion struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Weight      int    `json:"weight"`
}

type DraftInput struct {
	Title              string                `json:"title"`
	Description        string                `json:"description"`
	ExpertType         string                `json:"expertType"`
	Language           string                `json:"language"`
	OverviewBudget     string                `json:"overviewBudget"`
	FormalBudget       string                `json:"formalBudget"`
	ExternalCostCap    string                `json:"externalCostCap"`
	Deadline           time.Time             `json:"deadline"`
	Inputs             []string              `json:"inputs"`
	AllowedTools       []string              `json:"allowedTools"`
	Exclusions         []string              `json:"exclusions"`
	DeliveryFormat     string                `json:"deliveryFormat"`
	AcceptanceCriteria []AcceptanceCriterion `json:"acceptanceCriteria"`
}

type UpdateDraftInput struct {
	DraftInput
	ExpectedVersion int64 `json:"expectedVersion"`
}

type PublishInput struct {
	ExpectedVersion int64 `json:"expectedVersion"`
}

type Task struct {
	ID                       string                `json:"id"`
	PublisherID              string                `json:"publisherId"`
	Status                   string                `json:"status"`
	Title                    string                `json:"title"`
	Description              string                `json:"description"`
	ExpertType               string                `json:"expertType"`
	Language                 string                `json:"language"`
	OverviewBudget           string                `json:"overviewBudget"`
	FormalBudget             string                `json:"formalBudget"`
	ExternalCostCap          string                `json:"externalCostCap"`
	Deadline                 time.Time             `json:"deadline"`
	Inputs                   []string              `json:"inputs"`
	AllowedTools             []string              `json:"allowedTools"`
	Exclusions               []string              `json:"exclusions"`
	DeliveryFormat           string                `json:"deliveryFormat"`
	AcceptanceCriteria       []AcceptanceCriterion `json:"acceptanceCriteria"`
	AggregateVersion         int64                 `json:"aggregateVersion"`
	CurrentSpecVersion       *int                  `json:"currentSpecVersion,omitempty"`
	CurrentAcceptanceVersion *int                  `json:"currentAcceptanceVersion,omitempty"`
	PublishedAt              *time.Time            `json:"publishedAt,omitempty"`
	CreatedAt                time.Time             `json:"createdAt"`
	UpdatedAt                time.Time             `json:"updatedAt"`
}

type SpecVersion struct {
	TaskID               string    `json:"taskId"`
	Version              int       `json:"version"`
	TaskAggregateVersion int64     `json:"taskAggregateVersion"`
	ContentHash          string    `json:"contentHash"`
	Title                string    `json:"title"`
	Description          string    `json:"description"`
	ExpertType           string    `json:"expertType"`
	Language             string    `json:"language"`
	OverviewBudget       string    `json:"overviewBudget"`
	FormalBudget         string    `json:"formalBudget"`
	ExternalCostCap      string    `json:"externalCostCap"`
	Deadline             time.Time `json:"deadline"`
	Inputs               []string  `json:"inputs"`
	AllowedTools         []string  `json:"allowedTools"`
	Exclusions           []string  `json:"exclusions"`
	DeliveryFormat       string    `json:"deliveryFormat"`
	CreatedAt            time.Time `json:"createdAt"`
}

type AcceptanceVersion struct {
	TaskID               string                `json:"taskId"`
	Version              int                   `json:"version"`
	TaskAggregateVersion int64                 `json:"taskAggregateVersion"`
	ContentHash          string                `json:"contentHash"`
	Criteria             []AcceptanceCriterion `json:"criteria"`
	TotalWeight          int                   `json:"totalWeight"`
	CreatedAt            time.Time             `json:"createdAt"`
}

type Publication struct {
	Task       Task              `json:"task"`
	Spec       SpecVersion       `json:"spec"`
	Acceptance AcceptanceVersion `json:"acceptance"`
}

type Mutation struct {
	ActorID        string
	IdempotencyKey string
	RequestHash    string
	EventID        string
	Now            time.Time
}

type Store interface {
	Create(context.Context, Mutation, DraftInput, string) (Task, bool, error)
	UpdateDraft(context.Context, Mutation, string, UpdateDraftInput) (Task, bool, error)
	Publish(context.Context, Mutation, string, PublishInput) (Publication, bool, error)
	Get(context.Context, string, string) (Task, error)
}

type Service struct {
	store Store
	now   func() time.Time
}

func NewService(store Store) (*Service, error) {
	if store == nil {
		return nil, errors.New("task store is required")
	}
	return &Service{store: store, now: time.Now}, nil
}

func (s *Service) Create(ctx context.Context, session auth.Session, key string, input DraftInput) (Task, bool, error) {
	if !publisherAuthorized(session) {
		return Task{}, false, ErrForbidden
	}
	if err := validateDraft(input); err != nil {
		return Task{}, false, err
	}
	id, err := randomID("task")
	if err != nil {
		return Task{}, false, err
	}
	mutation, err := s.mutation(session, key, input)
	if err != nil {
		return Task{}, false, err
	}
	return s.store.Create(ctx, mutation, input, id)
}

func (s *Service) UpdateDraft(ctx context.Context, session auth.Session, key, id string, input UpdateDraftInput) (Task, bool, error) {
	if !publisherAuthorized(session) {
		return Task{}, false, ErrForbidden
	}
	if id == "" || input.ExpectedVersion < 1 {
		return Task{}, false, ErrStaleVersion
	}
	if err := validateDraft(input.DraftInput); err != nil {
		return Task{}, false, err
	}
	mutation, err := s.mutation(session, key, input)
	if err != nil {
		return Task{}, false, err
	}
	return s.store.UpdateDraft(ctx, mutation, id, input)
}

func (s *Service) Publish(ctx context.Context, session auth.Session, key, id string, input PublishInput) (Publication, bool, error) {
	if !publisherAuthorized(session) {
		return Publication{}, false, ErrForbidden
	}
	if id == "" || input.ExpectedVersion < 1 {
		return Publication{}, false, ErrStaleVersion
	}
	mutation, err := s.mutation(session, key, input)
	if err != nil {
		return Publication{}, false, err
	}
	return s.store.Publish(ctx, mutation, id, input)
}

func (s *Service) Get(ctx context.Context, session auth.Session, id string) (Task, error) {
	if !publisherAuthorized(session) {
		return Task{}, ErrForbidden
	}
	if id == "" {
		return Task{}, ErrNotFound
	}
	return s.store.Get(ctx, session.UserID, id)
}

func (s *Service) mutation(session auth.Session, key string, input any) (Mutation, error) {
	if key == "" || len(key) > 200 {
		return Mutation{}, ErrInvalidInput
	}
	body, err := json.Marshal(input)
	if err != nil {
		return Mutation{}, err
	}
	sum := sha256.Sum256(body)
	eventID, err := randomID("event")
	if err != nil {
		return Mutation{}, err
	}
	return Mutation{ActorID: session.UserID, IdempotencyKey: key, RequestHash: hex.EncodeToString(sum[:]), EventID: eventID, Now: s.now().UTC()}, nil
}

func PublicationVersions(value Task, version int, now time.Time) (SpecVersion, AcceptanceVersion, error) {
	specContent := struct {
		Title           string    `json:"title"`
		Description     string    `json:"description"`
		ExpertType      string    `json:"expertType"`
		Language        string    `json:"language"`
		OverviewBudget  string    `json:"overviewBudget"`
		FormalBudget    string    `json:"formalBudget"`
		ExternalCostCap string    `json:"externalCostCap"`
		Deadline        time.Time `json:"deadline"`
		Inputs          []string  `json:"inputs"`
		AllowedTools    []string  `json:"allowedTools"`
		Exclusions      []string  `json:"exclusions"`
		DeliveryFormat  string    `json:"deliveryFormat"`
	}{value.Title, value.Description, value.ExpertType, value.Language, value.OverviewBudget, value.FormalBudget, value.ExternalCostCap, value.Deadline, value.Inputs, value.AllowedTools, value.Exclusions, value.DeliveryFormat}
	specHash, err := contentHash(specContent)
	if err != nil {
		return SpecVersion{}, AcceptanceVersion{}, err
	}
	acceptanceHash, err := contentHash(value.AcceptanceCriteria)
	if err != nil {
		return SpecVersion{}, AcceptanceVersion{}, err
	}
	aggregateVersion := value.AggregateVersion + 1
	spec := SpecVersion{TaskID: value.ID, Version: version, TaskAggregateVersion: aggregateVersion, ContentHash: specHash, Title: value.Title, Description: value.Description, ExpertType: value.ExpertType, Language: value.Language, OverviewBudget: value.OverviewBudget, FormalBudget: value.FormalBudget, ExternalCostCap: value.ExternalCostCap, Deadline: value.Deadline, Inputs: value.Inputs, AllowedTools: value.AllowedTools, Exclusions: value.Exclusions, DeliveryFormat: value.DeliveryFormat, CreatedAt: now}
	acceptance := AcceptanceVersion{TaskID: value.ID, Version: version, TaskAggregateVersion: aggregateVersion, ContentHash: acceptanceHash, Criteria: value.AcceptanceCriteria, TotalWeight: 100, CreatedAt: now}
	return spec, acceptance, nil
}

func contentHash(value any) (string, error) {
	body, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(body)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

func validateDraft(input DraftInput) error {
	if blank(input.Title) || blank(input.Description) || blank(input.ExpertType) || blank(input.Language) || blank(input.DeliveryFormat) || input.Deadline.IsZero() {
		return ErrInvalidInput
	}
	if len(input.Title) > 200 || len(input.Description) > 50_000 || len(input.Inputs) > 100 || len(input.AllowedTools) > 100 || len(input.Exclusions) > 100 || len(input.AcceptanceCriteria) == 0 || len(input.AcceptanceCriteria) > 100 {
		return ErrInvalidInput
	}
	for _, amount := range []string{input.OverviewBudget, input.FormalBudget, input.ExternalCostCap} {
		if !canonicalUint(amount) {
			return ErrInvalidInput
		}
	}
	seen := make([]string, 0, len(input.AcceptanceCriteria))
	total := 0
	for _, criterion := range input.AcceptanceCriteria {
		if blank(criterion.ID) || blank(criterion.Title) || blank(criterion.Description) || criterion.Weight < 1 || criterion.Weight > 100 || slices.Contains(seen, criterion.ID) {
			return ErrInvalidInput
		}
		seen = append(seen, criterion.ID)
		total += criterion.Weight
	}
	if total != 100 {
		return ErrInvalidInput
	}
	for _, values := range [][]string{input.Inputs, input.AllowedTools, input.Exclusions} {
		for _, value := range values {
			if blank(value) || len(value) > 2_000 {
				return ErrInvalidInput
			}
		}
	}
	return nil
}

func canonicalUint(value string) bool {
	if len(value) > 78 {
		return false
	}
	if value == "0" {
		return true
	}
	if value == "" || value[0] == '0' {
		return false
	}
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func blank(value string) bool { return strings.TrimSpace(value) == "" }

func publisherAuthorized(session auth.Session) bool {
	return slices.Contains(session.Roles, "publisher")
}

func randomID(prefix string) (string, error) {
	const alphabet = "0123456789abcdefghijklmnopqrstuvwxyz"
	buffer := make([]byte, 24)
	for index := range buffer {
		value, err := rand.Int(rand.Reader, big.NewInt(int64(len(alphabet))))
		if err != nil {
			return "", err
		}
		buffer[index] = alphabet[value.Int64()]
	}
	return prefix + "_" + string(buffer), nil
}
