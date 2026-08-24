package agent

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

	"github.com/example/agent-platform/engine/internal/action"
	"github.com/example/agent-platform/engine/internal/auth"
	"github.com/example/agent-platform/engine/internal/credential"
)

var (
	ErrForbidden              = errors.New("agent operation forbidden")
	ErrNotFound               = errors.New("agent not found")
	ErrStaleVersion           = errors.New("stale agent aggregate version")
	ErrInvalidState           = errors.New("invalid agent state transition")
	ErrInvalidInput           = errors.New("invalid agent input")
	ErrInvalidPrice           = errors.New("invalid agent price")
	ErrCapacityUnavailable    = errors.New("agent capacity unavailable")
	ErrHealthCheckUnavailable = errors.New("agent health checker unavailable")
)

const (
	StatusDraft            = "draft"
	StatusActive           = "active"
	StatusPaused           = "paused"
	StatusRetired          = "retired"
	HealthUnknown          = "unknown"
	HealthHealthy          = "healthy"
	HealthDegraded         = "degraded"
	HealthUnhealthy        = "unhealthy"
	IncludedFormalVersions = 3
	MaxFormalVersions      = 5
	HealthFreshnessTTL     = 5 * time.Minute
	HealthFutureTolerance  = 30 * time.Second
)

type Agent struct {
	ID                       string     `json:"id"`
	OwnerID                  string     `json:"ownerId"`
	Name                     string     `json:"name"`
	Category                 string     `json:"category"`
	Tags                     []string   `json:"tags"`
	Capabilities             string     `json:"capabilities"`
	Languages                []string   `json:"languages"`
	EstimatedDurationSeconds int64      `json:"estimatedDurationSeconds"`
	AuthorBio                string     `json:"authorBio"`
	EndpointURL              string     `json:"endpointUrl"`
	ControllerAddress        string     `json:"controllerAddress"`
	PayoutAddress            string     `json:"payoutAddress"`
	Status                   string     `json:"status"`
	Health                   string     `json:"health"`
	HealthCheckedAt          *time.Time `json:"healthCheckedAt,omitempty"`
	HealthValidUntil         *time.Time `json:"healthValidUntil,omitempty"`
	MaxConcurrency           int        `json:"maxConcurrency"`
	ActiveCapacity           int        `json:"activeCapacity"`
	AggregateVersion         int64      `json:"aggregateVersion"`
	ActivatedAt              *time.Time `json:"activatedAt,omitempty"`
	CurrentPriceVersion      *int       `json:"currentPriceVersion,omitempty"`
	CurrentCredentialVersion *int       `json:"currentCredentialVersion,omitempty"`
	CreatedAt                time.Time  `json:"createdAt"`
	UpdatedAt                time.Time  `json:"updatedAt"`
}

type PriceVersion struct {
	AgentID                 string    `json:"agentId"`
	Version                 int       `json:"version"`
	AgentAggregateVersion   int64     `json:"agentAggregateVersion"`
	OverviewPrice           string    `json:"overviewPrice"`
	FormalPackageGrossPrice string    `json:"formalPackageGrossPrice"`
	AdditionalVersionPrice  string    `json:"additionalVersionPrice"`
	ExternalCostCap         string    `json:"externalCostCap"`
	IncludedVersions        int       `json:"includedVersions"`
	MaxVersions             int       `json:"maxVersions"`
	CreatedAt               time.Time `json:"createdAt"`
}
type CapacityLease struct {
	ReservationID string    `json:"reservationId"`
	AgentID       string    `json:"agentId"`
	FencingToken  int64     `json:"fencingToken"`
	ExpiresAt     time.Time `json:"expiresAt"`
}

type View struct {
	Agent            Agent           `json:"agent"`
	AvailableActions action.Response `json:"availableActions"`
}

type CreateInput struct {
	Name                     string   `json:"name"`
	Category                 string   `json:"category"`
	Tags                     []string `json:"tags"`
	Capabilities             string   `json:"capabilities"`
	Languages                []string `json:"languages"`
	EstimatedDurationSeconds int64    `json:"estimatedDurationSeconds"`
	AuthorBio                string   `json:"authorBio"`
	EndpointURL              string   `json:"endpointUrl"`
	ControllerAddress        string   `json:"controllerAddress"`
	PayoutAddress            string   `json:"payoutAddress"`
	MaxConcurrency           int      `json:"maxConcurrency"`
}
type ProfileInput struct {
	CreateInput
	ExpectedVersion int64 `json:"expectedVersion"`
}
type LifecycleInput struct {
	Status          string `json:"status"`
	ExpectedVersion int64  `json:"expectedVersion"`
}
type HealthInput struct {
	Health          string    `json:"health"`
	ExpectedVersion int64     `json:"expectedVersion"`
	CheckedAt       time.Time `json:"checkedAt"`
}
type HealthCheckInput struct {
	ExpectedVersion int64 `json:"expectedVersion"`
}
type CapacityInput struct {
	MaxConcurrency  int   `json:"maxConcurrency"`
	ExpectedVersion int64 `json:"expectedVersion"`
}
type PriceInput struct {
	OverviewPrice           string `json:"overviewPrice"`
	FormalPackageGrossPrice string `json:"formalPackageGrossPrice"`
	AdditionalVersionPrice  string `json:"additionalVersionPrice"`
	ExternalCostCap         string `json:"externalCostCap"`
	ExpectedVersion         int64  `json:"expectedVersion"`
}

type Mutation struct {
	ActorID        string
	IdempotencyKey string
	RequestHash    string
	EventID        string
	Now            time.Time
}
type Store interface {
	Create(context.Context, Mutation, CreateInput, string) (Agent, bool, error)
	UpdateProfile(context.Context, Mutation, string, ProfileInput) (Agent, bool, error)
	Transition(context.Context, Mutation, string, LifecycleInput) (Agent, bool, error)
	UpdateHealth(context.Context, Mutation, string, HealthInput) (Agent, bool, error)
	CheckHealth(context.Context, Mutation, string, HealthCheckInput, func(context.Context, string) error) (Agent, bool, error)
	UpdateCapacity(context.Context, Mutation, string, CapacityInput) (Agent, bool, error)
	PublishPrice(context.Context, Mutation, string, PriceInput) (PriceVersion, bool, error)
	Get(context.Context, string, string) (Agent, error)
	GetForActions(context.Context, string, string) (Agent, time.Time, error)
	ReserveCapacity(context.Context, string, string, time.Time) (CapacityLease, error)
	ReleaseCapacity(context.Context, string, int64) error
}

type HealthChecker interface {
	Check(context.Context, string) error
}

type Service struct {
	store         Store
	healthChecker HealthChecker
	now           func() time.Time
}

func NewService(store Store) (*Service, error) {
	return NewServiceWithHealthChecker(store, nil)
}

func NewServiceWithHealthChecker(store Store, healthChecker HealthChecker) (*Service, error) {
	if store == nil {
		return nil, errors.New("agent store is required")
	}
	return &Service{store: store, healthChecker: healthChecker, now: time.Now}, nil
}

func (s *Service) Create(ctx context.Context, session auth.Session, key string, input CreateInput) (Agent, bool, error) {
	if !hasRole(session, "agent_provider") {
		return Agent{}, false, ErrForbidden
	}
	if err := validateCreate(input); err != nil {
		return Agent{}, false, err
	}
	id, err := randomID("agent")
	if err != nil {
		return Agent{}, false, err
	}
	mutation, err := s.mutation(session, key, input)
	if err != nil {
		return Agent{}, false, err
	}
	return s.store.Create(ctx, mutation, input, id)
}
func (s *Service) UpdateProfile(ctx context.Context, session auth.Session, key, id string, input ProfileInput) (Agent, bool, error) {
	if !hasRole(session, "agent_provider") {
		return Agent{}, false, ErrForbidden
	}
	if input.ExpectedVersion < 1 {
		return Agent{}, false, ErrStaleVersion
	}
	if err := validateCreate(input.CreateInput); err != nil {
		return Agent{}, false, err
	}
	m, e := s.mutation(session, key, input)
	if e != nil {
		return Agent{}, false, e
	}
	return s.store.UpdateProfile(ctx, m, id, input)
}
func (s *Service) Transition(ctx context.Context, session auth.Session, key, id string, input LifecycleInput) (Agent, bool, error) {
	if !hasRole(session, "agent_provider") {
		return Agent{}, false, ErrForbidden
	}
	if input.ExpectedVersion < 1 || !slices.Contains([]string{StatusDraft, StatusActive, StatusPaused, StatusRetired}, input.Status) {
		return Agent{}, false, ErrInvalidState
	}
	m, e := s.mutation(session, key, input)
	if e != nil {
		return Agent{}, false, e
	}
	return s.store.Transition(ctx, m, id, input)
}
func (s *Service) UpdateHealth(ctx context.Context, session auth.Session, key, id string, input HealthInput) (Agent, bool, error) {
	if !hasRole(session, "agent_provider") {
		return Agent{}, false, ErrForbidden
	}
	if input.ExpectedVersion < 1 || !slices.Contains([]string{HealthUnknown, HealthHealthy, HealthDegraded, HealthUnhealthy}, input.Health) {
		return Agent{}, false, ErrInvalidInput
	}
	m, e := s.mutation(session, key, input)
	if e != nil {
		return Agent{}, false, e
	}
	if input.CheckedAt.IsZero() {
		input.CheckedAt = m.Now
	}
	if input.CheckedAt.After(m.Now.Add(HealthFutureTolerance)) || input.CheckedAt.Before(m.Now.Add(-HealthFreshnessTTL)) {
		return Agent{}, false, ErrInvalidInput
	}
	return s.store.UpdateHealth(ctx, m, id, input)
}

// CheckHealth records only an Engine-observed protocol result. The caller can
// request a check, but cannot supply either the result or its timestamp.
func (s *Service) CheckHealth(ctx context.Context, session auth.Session, key, id string, input HealthCheckInput) (Agent, bool, error) {
	if !hasRole(session, "agent_provider") {
		return Agent{}, false, ErrForbidden
	}
	if input.ExpectedVersion < 1 {
		return Agent{}, false, ErrInvalidInput
	}
	if s.healthChecker == nil {
		return Agent{}, false, ErrHealthCheckUnavailable
	}
	m, err := s.mutation(session, key, input)
	if err != nil {
		return Agent{}, false, err
	}
	return s.store.CheckHealth(ctx, m, id, input, s.healthChecker.Check)
}
func (s *Service) UpdateCapacity(ctx context.Context, session auth.Session, key, id string, input CapacityInput) (Agent, bool, error) {
	if !hasRole(session, "agent_provider") {
		return Agent{}, false, ErrForbidden
	}
	if input.ExpectedVersion < 1 || input.MaxConcurrency < 1 || input.MaxConcurrency > 10000 {
		return Agent{}, false, ErrInvalidInput
	}
	m, e := s.mutation(session, key, input)
	if e != nil {
		return Agent{}, false, e
	}
	return s.store.UpdateCapacity(ctx, m, id, input)
}
func (s *Service) PublishPrice(ctx context.Context, session auth.Session, key, id string, input PriceInput) (PriceVersion, bool, error) {
	if !hasRole(session, "agent_provider") {
		return PriceVersion{}, false, ErrForbidden
	}
	if input.ExpectedVersion < 1 || !validPrices(input) {
		return PriceVersion{}, false, ErrInvalidPrice
	}
	m, e := s.mutation(session, key, input)
	if e != nil {
		return PriceVersion{}, false, e
	}
	return s.store.PublishPrice(ctx, m, id, input)
}
func (s *Service) Get(ctx context.Context, session auth.Session, id string) (Agent, error) {
	if !hasRole(session, "agent_provider") {
		return Agent{}, ErrForbidden
	}
	return s.store.Get(ctx, session.UserID, id)
}

func (s *Service) AvailableActions(ctx context.Context, session auth.Session, id string) (action.Response, error) {
	view, err := s.View(ctx, session, id)
	return view.AvailableActions, err
}

func (s *Service) View(ctx context.Context, session auth.Session, id string) (View, error) {
	if !hasRole(session, "agent_provider") {
		return View{}, ErrForbidden
	}
	value, now, err := s.store.GetForActions(ctx, session.UserID, id)
	if err != nil {
		return View{}, err
	}
	notRetired := []action.Reason{}
	if value.Status == StatusRetired {
		notRetired = append(notRetired, action.Because("agent_retired", "Retired agents are immutable."))
	}
	addressReasons := append([]action.Reason(nil), notRetired...)
	if value.ActivatedAt != nil {
		addressReasons = append(addressReasons, action.Because("addresses_frozen", "Controller and payout addresses are frozen after first activation."))
	}
	if value.Status != StatusDraft && value.Status != StatusPaused {
		addressReasons = append(addressReasons, action.Because("address_state_not_editable", "Addresses can only be changed while draft or paused."))
	}
	activateReasons := []action.Reason{}
	if value.Status != StatusDraft && value.Status != StatusPaused {
		activateReasons = append(activateReasons, action.Because("activation_transition_not_allowed", "Only draft or paused agents can become active."))
	}
	if value.CurrentPriceVersion == nil {
		activateReasons = append(activateReasons, action.Because("price_required", "A published price version is required before activation."))
	}
	if value.Health != HealthHealthy {
		activateReasons = append(activateReasons, action.Because("healthy_status_required", "A healthy status is required before activation."))
	}
	if value.HealthValidUntil == nil || !now.Before(*value.HealthValidUntil) {
		activateReasons = append(activateReasons, action.Because("health_check_expired", "A current health check is required before activation."))
	}
	pauseReasons := []action.Reason{}
	if value.Status != StatusDraft && value.Status != StatusActive {
		pauseReasons = append(pauseReasons, action.Because("pause_transition_not_allowed", "Only draft or active agents can become paused."))
	}
	returnToDraftReasons := []action.Reason{}
	if value.Status != StatusPaused {
		returnToDraftReasons = append(returnToDraftReasons, action.Because("draft_transition_not_allowed", "Only paused agents can return to draft."))
	}
	retireReasons := []action.Reason{}
	if value.Status == StatusRetired {
		retireReasons = append(retireReasons, action.Because("agent_retired", "The agent is already retired."))
	}
	if value.ActiveCapacity > 0 {
		retireReasons = append(retireReasons, action.Because("active_capacity_nonzero", "Release active capacity before retiring the agent."))
	}
	credentialReasons := append([]action.Reason(nil), notRetired...)
	if !credential.CanRotate(session) {
		credentialReasons = append(credentialReasons, action.Because("credential_role_forbidden", "Admin and arbitrator sessions cannot rotate Agent credentials."))
	}
	return View{
		Agent: value,
		AvailableActions: action.Response{
			ResourceType: "agent", ResourceID: value.ID, AggregateVersion: value.AggregateVersion,
			Actions: []action.Decision{
				action.Decide("update_profile", notRetired...),
				action.Decide("update_addresses", addressReasons...),
				action.Decide("update_health", notRetired...),
				action.Decide("update_capacity", notRetired...),
				action.Decide("publish_price", notRetired...),
				action.Decide("rotate_credential", credentialReasons...),
				action.Decide("activate", activateReasons...),
				action.Decide("pause", pauseReasons...),
				action.Decide("retire", retireReasons...),
				action.Decide("return_to_draft", returnToDraftReasons...),
			},
		},
	}, nil
}
func (s *Service) ReserveCapacity(ctx context.Context, agentID, reservationID string, ttl time.Duration) (CapacityLease, error) {
	if reservationID == "" || ttl <= 0 || ttl > time.Hour {
		return CapacityLease{}, ErrInvalidInput
	}
	return s.store.ReserveCapacity(ctx, agentID, reservationID, s.now().UTC().Add(ttl))
}
func (s *Service) ReleaseCapacity(ctx context.Context, reservationID string, fencingToken int64) error {
	if reservationID == "" || fencingToken < 1 {
		return ErrInvalidInput
	}
	return s.store.ReleaseCapacity(ctx, reservationID, fencingToken)
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
func validateCreate(i CreateInput) error {
	if strings.TrimSpace(i.Name) == "" || len(i.Name) > 200 || strings.TrimSpace(i.Category) == "" || len(i.Category) > 100 || strings.TrimSpace(i.Capabilities) == "" || len(i.Capabilities) > 5000 || !ValidEndpointURL(i.EndpointURL) || i.EstimatedDurationSeconds < 1 || i.MaxConcurrency < 1 || i.MaxConcurrency > 10000 || !auth.IsWalletAddress(strings.ToLower(i.ControllerAddress)) || !auth.IsWalletAddress(strings.ToLower(i.PayoutAddress)) || len(i.Tags) > 50 || len(i.Languages) == 0 || len(i.Languages) > 20 {
		return ErrInvalidInput
	}
	return nil
}
func validPrices(i PriceInput) bool {
	overview, ok := nonnegative(i.OverviewPrice)
	if !ok {
		return false
	}
	formal, ok := nonnegative(i.FormalPackageGrossPrice)
	if !ok || overview.Cmp(formal) > 0 {
		return false
	}
	_, ok = nonnegative(i.AdditionalVersionPrice)
	if !ok {
		return false
	}
	_, ok = nonnegative(i.ExternalCostCap)
	return ok
}
func nonnegative(value string) (*big.Int, bool) {
	if value == "" || strings.HasPrefix(value, "+") || len(value) > 78 {
		return nil, false
	}
	number, ok := new(big.Int).SetString(value, 10)
	return number, ok && number.Sign() >= 0 && number.String() == value
}
func hasRole(session auth.Session, role string) bool { return slices.Contains(session.Roles, role) }
func randomID(prefix string) (string, error) {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return prefix + "_" + hex.EncodeToString(value), nil
}
