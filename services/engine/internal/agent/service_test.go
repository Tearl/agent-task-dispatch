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
func (*testStore) Get(context.Context, string, string) (Agent, error) { return Agent{}, nil }
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

func validCreateInput() CreateInput {
	return CreateInput{
		Name:                     "Research Agent",
		Category:                 "research",
		Tags:                     []string{"analysis"},
		Capabilities:             "Produces structured research",
		Languages:                []string{"zh-CN", "en"},
		EstimatedDurationSeconds: 300,
		AuthorBio:                "Provider",
		ControllerAddress:        "0x1111111111111111111111111111111111111111",
		PayoutAddress:            "0x2222222222222222222222222222222222222222",
		MaxConcurrency:           2,
	}
}
