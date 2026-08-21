package agent

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/example/agent-platform/engine/internal/auth"
)

type testStore struct {
	createCalls     int
	priceCalls      int
	lastMutation    Mutation
	healthMutations []Mutation
	healthInputs    []HealthInput
	getAgent        Agent
	getErr          error
	databaseNow     time.Time
}

type testHealthChecker struct {
	calls   []string
	results []error
}

func (c *testHealthChecker) Check(_ context.Context, endpoint string) error {
	c.calls = append(c.calls, endpoint)
	if len(c.results) == 0 {
		return nil
	}
	result := c.results[0]
	c.results = c.results[1:]
	return result
}

func (s *testStore) Create(_ context.Context, mutation Mutation, _ CreateInput, id string) (Agent, bool, error) {
	s.createCalls++
	s.lastMutation = mutation
	return Agent{ID: id, OwnerID: mutation.ActorID, AggregateVersion: 1}, false, nil
}
func (*testStore) UpdateProfile(context.Context, Mutation, string, ProfileInput) (Agent, bool, error) {
	return Agent{}, false, nil
}
func (*testStore) Transition(context.Context, Mutation, string, LifecycleInput) (Agent, bool, error) {
	return Agent{}, false, nil
}
func (s *testStore) UpdateHealth(_ context.Context, mutation Mutation, _ string, input HealthInput) (Agent, bool, error) {
	s.healthMutations = append(s.healthMutations, mutation)
	s.healthInputs = append(s.healthInputs, input)
	return Agent{}, false, nil
}
func (*testStore) UpdateCapacity(context.Context, Mutation, string, CapacityInput) (Agent, bool, error) {
	return Agent{}, false, nil
}
func (s *testStore) PublishPrice(_ context.Context, mutation Mutation, _ string, _ PriceInput) (PriceVersion, bool, error) {
	s.priceCalls++
	s.lastMutation = mutation
	return PriceVersion{}, false, nil
}
func (s *testStore) Get(context.Context, string, string) (Agent, error) { return s.getAgent, s.getErr }
func (s *testStore) GetForActions(context.Context, string, string) (Agent, time.Time, error) {
	return s.getAgent, s.databaseNow, s.getErr
}
func (*testStore) ReserveCapacity(context.Context, string, string, time.Time) (CapacityLease, error) {
	return CapacityLease{}, nil
}
func (*testStore) ReleaseCapacity(context.Context, string, int64) error { return nil }

func TestAgentMutationsRequireProviderRoleAndIdempotencyKey(t *testing.T) {
	store := &testStore{}
	service, err := NewService(store)
	if err != nil {
		t.Fatal(err)
	}
	input := validCreateInput()
	publisher := auth.Session{UserID: "publisher", Roles: []string{"publisher"}}
	if _, _, err = service.Create(context.Background(), publisher, "create-1", input); !errors.Is(err, ErrForbidden) {
		t.Fatalf("publisher create: %v", err)
	}
	admin := auth.Session{UserID: "admin", Roles: []string{"admin"}}
	if _, _, err = service.Create(context.Background(), admin, "create-1", input); !errors.Is(err, ErrForbidden) {
		t.Fatalf("admin create: %v", err)
	}
	provider := auth.Session{UserID: "provider", Roles: []string{"agent_provider"}}
	if _, _, err = service.Create(context.Background(), provider, "", input); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("missing idempotency key: %v", err)
	}
	created, replay, err := service.Create(context.Background(), provider, "create-1", input)
	if err != nil || replay || created.OwnerID != provider.UserID || store.createCalls != 1 {
		t.Fatalf("provider create: created=%#v replay=%v calls=%d err=%v", created, replay, store.createCalls, err)
	}
	if store.lastMutation.ActorID != provider.UserID || store.lastMutation.IdempotencyKey != "create-1" || store.lastMutation.RequestHash == "" || store.lastMutation.EventID == "" {
		t.Fatalf("mutation context incomplete: %#v", store.lastMutation)
	}
}

func TestEveryOwnerOperationRejectsNonProviderRoles(t *testing.T) {
	service, err := NewService(&testStore{})
	if err != nil {
		t.Fatal(err)
	}
	for _, session := range []auth.Session{
		{UserID: "publisher", Roles: []string{"publisher"}},
		{UserID: "admin", Roles: []string{"admin"}},
		{UserID: "arbitrator", Roles: []string{"arbitrator"}},
	} {
		checks := []struct {
			name string
			run  func() error
		}{
			{name: "profile", run: func() error {
				_, _, callErr := service.UpdateProfile(context.Background(), session, "key", "agent", ProfileInput{CreateInput: validCreateInput(), ExpectedVersion: 1})
				return callErr
			}},
			{name: "lifecycle", run: func() error {
				_, _, callErr := service.Transition(context.Background(), session, "key", "agent", LifecycleInput{Status: StatusPaused, ExpectedVersion: 1})
				return callErr
			}},
			{name: "health", run: func() error {
				_, _, callErr := service.UpdateHealth(context.Background(), session, "key", "agent", HealthInput{Health: HealthHealthy, ExpectedVersion: 1})
				return callErr
			}},
			{name: "capacity", run: func() error {
				_, _, callErr := service.UpdateCapacity(context.Background(), session, "key", "agent", CapacityInput{MaxConcurrency: 2, ExpectedVersion: 1})
				return callErr
			}},
			{name: "price", run: func() error {
				_, _, callErr := service.PublishPrice(context.Background(), session, "key", "agent", PriceInput{OverviewPrice: "1", FormalPackageGrossPrice: "2", AdditionalVersionPrice: "0", ExternalCostCap: "0", ExpectedVersion: 1})
				return callErr
			}},
			{name: "get", run: func() error { _, callErr := service.Get(context.Background(), session, "agent"); return callErr }},
		}
		for _, check := range checks {
			t.Run(session.UserID+"/"+check.name, func(t *testing.T) {
				if callErr := check.run(); !errors.Is(callErr, ErrForbidden) {
					t.Fatalf("expected forbidden, got %v", callErr)
				}
			})
		}
	}
}

func TestAgentInputAndPriceBoundaries(t *testing.T) {
	store := &testStore{}
	service, err := NewService(store)
	if err != nil {
		t.Fatal(err)
	}
	provider := auth.Session{UserID: "provider", Roles: []string{"agent_provider"}}
	invalidCreate := validCreateInput()
	invalidCreate.MaxConcurrency = 0
	if _, _, err = service.Create(context.Background(), provider, "invalid", invalidCreate); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("invalid create: %v", err)
	}
	valid := PriceInput{OverviewPrice: "0", FormalPackageGrossPrice: "10", AdditionalVersionPrice: "0", ExternalCostCap: "0", ExpectedVersion: 1}
	tests := []struct {
		name  string
		alter func(*PriceInput)
	}{
		{name: "negative overview", alter: func(i *PriceInput) { i.OverviewPrice = "-1" }},
		{name: "overview exceeds gross", alter: func(i *PriceInput) { i.OverviewPrice = "11" }},
		{name: "negative gross", alter: func(i *PriceInput) { i.FormalPackageGrossPrice = "-1" }},
		{name: "negative additional", alter: func(i *PriceInput) { i.AdditionalVersionPrice = "-1" }},
		{name: "negative external", alter: func(i *PriceInput) { i.ExternalCostCap = "-1" }},
		{name: "non canonical integer", alter: func(i *PriceInput) { i.OverviewPrice = "01" }},
		{name: "missing version", alter: func(i *PriceInput) { i.ExpectedVersion = 0 }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			input := valid
			test.alter(&input)
			if _, _, publishErr := service.PublishPrice(context.Background(), provider, "price", "agent-1", input); !errors.Is(publishErr, ErrInvalidPrice) {
				t.Fatalf("expected invalid price, got %v", publishErr)
			}
		})
	}
	if _, _, err = service.PublishPrice(context.Background(), provider, "price", "agent-1", valid); err != nil || store.priceCalls != 1 {
		t.Fatalf("valid price: calls=%d err=%v", store.priceCalls, err)
	}
}

func TestCapacityLeaseInputBounds(t *testing.T) {
	service, err := NewService(&testStore{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = service.ReserveCapacity(context.Background(), "agent", "", time.Minute); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("empty reservation: %v", err)
	}
	if _, err = service.ReserveCapacity(context.Background(), "agent", "reservation", time.Hour+time.Nanosecond); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("oversized ttl: %v", err)
	}
	if err = service.ReleaseCapacity(context.Background(), "reservation", 0); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("invalid fencing token: %v", err)
	}
}

func TestHealthDefaultTimestampKeepsOriginalRequestHashStable(t *testing.T) {
	store := &testStore{}
	service, err := NewService(store)
	if err != nil {
		t.Fatal(err)
	}
	times := []time.Time{
		time.Date(2026, 8, 21, 1, 0, 0, 0, time.UTC),
		time.Date(2026, 8, 21, 1, 1, 0, 0, time.UTC),
	}
	service.now = func() time.Time {
		value := times[0]
		times = times[1:]
		return value
	}
	session := auth.Session{UserID: "provider", Roles: []string{"agent_provider"}}
	input := HealthInput{Health: HealthHealthy, ExpectedVersion: 1}
	for range 2 {
		if _, _, err = service.UpdateHealth(context.Background(), session, "health-1", "agent-1", input); err != nil {
			t.Fatal(err)
		}
	}
	if len(store.healthMutations) != 2 || store.healthMutations[0].RequestHash != store.healthMutations[1].RequestHash {
		t.Fatalf("same logical request produced unstable hashes: %#v", store.healthMutations)
	}
	if store.healthInputs[0].CheckedAt.IsZero() || store.healthInputs[1].CheckedAt.IsZero() {
		t.Fatalf("service timestamps were not applied: %#v", store.healthInputs)
	}
}

func TestHealthRejectsUntrustedFutureAndStaleTimestamps(t *testing.T) {
	store := &testStore{}
	service, err := NewService(store)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 21, 1, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return now }
	session := auth.Session{UserID: "provider", Roles: []string{"agent_provider"}}
	for name, checkedAt := range map[string]time.Time{
		"future": now.Add(HealthFutureTolerance + time.Second),
		"stale":  now.Add(-HealthFreshnessTTL - time.Second),
	} {
		t.Run(name, func(t *testing.T) {
			_, _, callErr := service.UpdateHealth(context.Background(), session, "health-"+name, "agent-1", HealthInput{Health: HealthHealthy, ExpectedVersion: 1, CheckedAt: checkedAt})
			if !errors.Is(callErr, ErrInvalidInput) {
				t.Fatalf("expected invalid timestamp, got %v", callErr)
			}
		})
	}
	if len(store.healthInputs) != 0 {
		t.Fatalf("invalid health timestamp reached the store: %#v", store.healthInputs)
	}
}

func TestCheckHealthUsesOnlyEngineObservedProtocolResult(t *testing.T) {
	store := &testStore{getAgent: Agent{ID: "agent-1", OwnerID: "provider", EndpointURL: "https://agent.example/health", AggregateVersion: 3}}
	checker := &testHealthChecker{results: []error{nil, errors.New("unreachable")}}
	service, err := NewServiceWithHealthChecker(store, checker)
	if err != nil {
		t.Fatal(err)
	}
	session := auth.Session{UserID: "provider", Roles: []string{"agent_provider"}}
	input := HealthCheckInput{ExpectedVersion: 3}
	if _, _, err = service.CheckHealth(context.Background(), session, "check-1", "agent-1", input); err != nil {
		t.Fatal(err)
	}
	if _, _, err = service.CheckHealth(context.Background(), session, "check-1", "agent-1", input); err != nil {
		t.Fatal(err)
	}
	if len(checker.calls) != 2 || checker.calls[0] != store.getAgent.EndpointURL {
		t.Fatalf("unexpected protocol checks: %#v", checker.calls)
	}
	if len(store.healthInputs) != 2 || store.healthInputs[0].Health != HealthHealthy || store.healthInputs[1].Health != HealthUnhealthy {
		t.Fatalf("Engine did not derive health results: %#v", store.healthInputs)
	}
	if store.healthMutations[0].RequestHash != store.healthMutations[1].RequestHash {
		t.Fatalf("same check request produced unstable idempotency hashes: %#v", store.healthMutations)
	}
}

func TestCheckHealthRejectsUntrustedCallersAndMissingChecker(t *testing.T) {
	store := &testStore{getAgent: Agent{ID: "agent-1", EndpointURL: "https://agent.example/health"}}
	checker := &testHealthChecker{}
	service, _ := NewServiceWithHealthChecker(store, checker)
	if _, _, err := service.CheckHealth(context.Background(), auth.Session{UserID: "publisher", Roles: []string{"publisher"}}, "check", "agent-1", HealthCheckInput{ExpectedVersion: 1}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("publisher health check: %v", err)
	}
	if len(checker.calls) != 0 {
		t.Fatal("forbidden request reached health checker")
	}
	withoutChecker, _ := NewService(store)
	if _, _, err := withoutChecker.CheckHealth(context.Background(), auth.Session{UserID: "provider", Roles: []string{"agent_provider"}}, "check", "agent-1", HealthCheckInput{ExpectedVersion: 1}); !errors.Is(err, ErrHealthCheckUnavailable) {
		t.Fatalf("missing checker: %v", err)
	}
}

func TestAgentAvailableActionsIncludeAuthoritativeBlockingReasons(t *testing.T) {
	now := time.Date(2026, 8, 21, 4, 0, 0, 0, time.UTC)
	store := &testStore{databaseNow: now, getAgent: Agent{ID: "agent-1", OwnerID: "provider", Status: StatusDraft, Health: HealthUnknown, AggregateVersion: 7}}
	service, _ := NewService(store)
	service.now = func() time.Time { return now }
	provider := auth.Session{UserID: "provider", Roles: []string{"agent_provider"}}
	response, err := service.AvailableActions(context.Background(), provider, "agent-1")
	if err != nil || response.ResourceType != "agent" || response.AggregateVersion != 7 || len(response.Actions) != 10 {
		t.Fatalf("available actions: %#v err=%v", response, err)
	}
	if response.Actions[9].Allowed || len(response.Actions[9].Reasons) != 1 || response.Actions[9].Reasons[0].Code != "draft_transition_not_allowed" {
		t.Fatalf("draft return decision: %#v", response.Actions[9])
	}
	activate := response.Actions[6]
	if activate.Allowed || len(activate.Reasons) != 3 || activate.Reasons[0].Code != "price_required" || activate.Reasons[1].Code != "healthy_status_required" || activate.Reasons[2].Code != "health_check_expired" {
		t.Fatalf("activation reasons: %#v", activate)
	}
	priceVersion := 1
	healthValidUntil := now.Add(time.Minute)
	store.getAgent.Status = StatusPaused
	store.getAgent.Health = HealthHealthy
	store.getAgent.HealthValidUntil = &healthValidUntil
	store.getAgent.CurrentPriceVersion = &priceVersion
	service.now = func() time.Time { return now.Add(24 * time.Hour) }
	response, err = service.AvailableActions(context.Background(), provider, "agent-1")
	if err != nil || !response.Actions[6].Allowed || !response.Actions[9].Allowed {
		t.Fatalf("database-current health should allow activation: %#v err=%v", response.Actions[6], err)
	}
	store.getAgent.Status = StatusPaused
	store.getAgent.ActivatedAt = &now
	response, err = service.AvailableActions(context.Background(), provider, "agent-1")
	if err != nil {
		t.Fatal(err)
	}
	addresses := response.Actions[1]
	if addresses.Allowed || len(addresses.Reasons) != 1 || addresses.Reasons[0].Code != "addresses_frozen" {
		t.Fatalf("address reasons: %#v", addresses)
	}
	dualRole := auth.Session{UserID: "provider", Roles: []string{"agent_provider", "admin"}}
	response, err = service.AvailableActions(context.Background(), dualRole, "agent-1")
	if err != nil || response.Actions[5].Allowed || len(response.Actions[5].Reasons) != 1 || response.Actions[5].Reasons[0].Code != "credential_role_forbidden" {
		t.Fatalf("credential role decision: %#v err=%v", response.Actions[5], err)
	}
	store.getAgent.Status = StatusRetired
	store.getAgent.ActiveCapacity = 1
	response, err = service.AvailableActions(context.Background(), provider, "agent-1")
	if err != nil {
		t.Fatal(err)
	}
	if response.Actions[0].Allowed || response.Actions[8].Allowed || len(response.Actions[8].Reasons) != 2 || response.Actions[9].Allowed {
		t.Fatalf("retired decisions: %#v", response.Actions)
	}
	store.getAgent.Status = StatusActive
	store.getAgent.ActiveCapacity = 1
	view, err := service.View(context.Background(), provider, "agent-1")
	if err != nil || view.Agent.ActiveCapacity != 1 || view.AvailableActions.Actions[8].Allowed || view.AvailableActions.Actions[8].Reasons[0].Code != "active_capacity_nonzero" || view.AvailableActions.Actions[9].Allowed {
		t.Fatalf("single-snapshot capacity view: %#v err=%v", view, err)
	}
}

func validCreateInput() CreateInput {
	return CreateInput{
		Name:                     "Research Agent",
		Category:                 "research",
		Tags:                     []string{"analysis"},
		Capabilities:             "Produces structured research",
		Languages:                []string{"zh-CN", "en"},
		EstimatedDurationSeconds: 300,
		AuthorBio:                "Provider",
		EndpointURL:              "https://agent.example/health",
		ControllerAddress:        "0x1111111111111111111111111111111111111111",
		PayoutAddress:            "0x2222222222222222222222222222222222222222",
		MaxConcurrency:           2,
	}
}
