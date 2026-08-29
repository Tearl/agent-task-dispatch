//go:build integration

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/example/agent-platform/engine/internal/agent"
	"github.com/example/agent-platform/engine/internal/auth"
	"github.com/example/agent-platform/engine/internal/persistence"
	persistencepostgres "github.com/example/agent-platform/engine/internal/persistence/postgres"
	"github.com/lib/pq"
)

type countingHealthChecker struct{ calls int }

func (c *countingHealthChecker) Check(context.Context, string) error {
	c.calls++
	return nil
}

func TestPostgresAgentOwnershipLifecyclePricesIdempotencyAndCapacity(t *testing.T) {
	baseURL := os.Getenv("ENGINE_TEST_POSTGRES_URL")
	if baseURL == "" {
		t.Skip("ENGINE_TEST_POSTGRES_URL is required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	admin, err := sql.Open("postgres", baseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	schema := fmt.Sprintf("engine_t103_%d", time.Now().UnixNano())
	if _, err = admin.ExecContext(ctx, "CREATE SCHEMA "+pq.QuoteIdentifier(schema)); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = admin.ExecContext(context.Background(), "DROP SCHEMA "+pq.QuoteIdentifier(schema)+" CASCADE")
	}()
	db, err := sql.Open("postgres", agentSearchPath(baseURL, schema))
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(12)
	defer db.Close()
	if err = persistencepostgres.ApplyMigrations(ctx, db); err != nil {
		t.Fatal(err)
	}
	for _, userID := range []string{"owner-a", "owner-b", "publisher", "admin"} {
		if _, err = db.ExecContext(ctx, `INSERT INTO users (user_id) VALUES ($1)`, userID); err != nil {
			t.Fatal(err)
		}
	}
	store, err := NewStore(db)
	if err != nil {
		t.Fatal(err)
	}
	service, err := agent.NewService(store)
	if err != nil {
		t.Fatal(err)
	}
	ownerA := auth.Session{UserID: "owner-a", Roles: []string{"agent_provider"}}
	ownerB := auth.Session{UserID: "owner-b", Roles: []string{"agent_provider"}}
	input := integrationCreateInput()

	created, replay, err := service.Create(ctx, ownerA, "create-agent", input)
	if err != nil || replay || created.AggregateVersion != 1 || created.Status != agent.StatusDraft {
		t.Fatalf("create: agent=%#v replay=%v err=%v", created, replay, err)
	}
	actions, err := service.AvailableActions(ctx, ownerA, created.ID)
	if err != nil || actions.AggregateVersion != 1 || len(actions.Actions) != 10 || actions.Actions[6].Allowed || len(actions.Actions[6].Reasons) != 3 || actions.Actions[9].Allowed {
		t.Fatalf("draft available actions: %#v err=%v", actions, err)
	}
	if _, err = service.AvailableActions(ctx, ownerB, created.ID); !errors.Is(err, agent.ErrNotFound) {
		t.Fatalf("other owner available actions: %v", err)
	}
	authorityAgent, _, err := service.Create(ctx, ownerA, "create-authority-agent", input)
	if err != nil {
		t.Fatal(err)
	}
	authorityInput := agent.MatchingAuthorityInput{ApprovalStatus: "approved", RiskStatus: "eligible", MatchingVectorVersion: "matching-vector-v1", ReputationQuality: 91, ReputationSpeed: 82, ReputationReliability: 73, ReputationCommunication: 64, ReputationCompliance: 55, ExpectedVersion: authorityAgent.AggregateVersion}
	if _, _, err = service.UpdateMatchingAuthority(ctx, ownerA, "provider-authority", authorityAgent.ID, authorityInput); !errors.Is(err, agent.ErrForbidden) {
		t.Fatalf("provider changed matching authority: %v", err)
	}
	authority, replay, err := service.UpdateMatchingAuthority(ctx, auth.Session{UserID: "admin", Roles: []string{"admin"}}, "approve-authority", authorityAgent.ID, authorityInput)
	if err != nil || replay || authority.AgentAggregateVersion != 2 || authority.MatchingVectorVersion != "matching-vector-v1" {
		t.Fatalf("admin matching authority: authority=%#v replay=%v err=%v", authority, replay, err)
	}
	assertCount(t, db, `SELECT count(*) FROM agent_matching_authority_events WHERE agent_id=$1 AND actor_id='admin'`, authorityAgent.ID, 1)
	probeInput := input
	probeInput.Name = "Health Probe Agent"
	probeAgent, _, err := service.Create(ctx, ownerA, "create-health-probe-agent", probeInput)
	if err != nil {
		t.Fatal(err)
	}
	checker := &countingHealthChecker{}
	probeService, err := agent.NewServiceWithHealthChecker(store, checker)
	if err != nil {
		t.Fatal(err)
	}
	checked, replay, err := probeService.CheckHealth(ctx, ownerA, "probe-health", probeAgent.ID, agent.HealthCheckInput{ExpectedVersion: 1})
	if err != nil || replay || checked.AggregateVersion != 2 || checked.Health != agent.HealthHealthy {
		t.Fatalf("health probe: agent=%#v replay=%v err=%v", checked, replay, err)
	}
	checkedReplay, replay, err := probeService.CheckHealth(ctx, ownerA, "probe-health", probeAgent.ID, agent.HealthCheckInput{ExpectedVersion: 1})
	if err != nil || !replay || checkedReplay.AggregateVersion != 2 || checker.calls != 1 {
		t.Fatalf("health probe replay performed an external call: agent=%#v replay=%v calls=%d err=%v", checkedReplay, replay, checker.calls, err)
	}
	releaseProbeSlot, err := store.acquireHealthProbeSlot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	checkedBusyReplay, replay, err := probeService.CheckHealth(ctx, ownerA, "probe-health", probeAgent.ID, agent.HealthCheckInput{ExpectedVersion: 1})
	if err != nil || !replay || checkedBusyReplay.AggregateVersion != 2 || checker.calls != 1 {
		releaseProbeSlot()
		t.Fatalf("busy health probe admission blocked completed replay: agent=%#v replay=%v calls=%d err=%v", checkedBusyReplay, replay, checker.calls, err)
	}
	if _, replay, err = probeService.CheckHealth(ctx, ownerA, "probe-health-while-busy", probeAgent.ID, agent.HealthCheckInput{ExpectedVersion: 2}); !errors.Is(err, agent.ErrHealthCheckUnavailable) || replay || checker.calls != 1 {
		releaseProbeSlot()
		t.Fatalf("busy health probe admission allowed a new probe: replay=%v calls=%d err=%v", replay, checker.calls, err)
	}
	releaseProbeSlot()
	if _, _, err = probeService.CheckHealth(ctx, ownerA, "stale-probe-health", probeAgent.ID, agent.HealthCheckInput{ExpectedVersion: 1}); !errors.Is(err, agent.ErrStaleVersion) || checker.calls != 1 {
		t.Fatalf("stale health probe reached checker: calls=%d err=%v", checker.calls, err)
	}
	probeRetired, _, err := probeService.Transition(ctx, ownerA, "retire-health-probe-agent", probeAgent.ID, agent.LifecycleInput{Status: agent.StatusRetired, ExpectedVersion: 2})
	if err != nil || probeRetired.AggregateVersion != 3 {
		t.Fatalf("retire probe Agent: agent=%#v err=%v", probeRetired, err)
	}
	if _, _, err = probeService.CheckHealth(ctx, ownerA, "retired-probe-health", probeAgent.ID, agent.HealthCheckInput{ExpectedVersion: 3}); !errors.Is(err, agent.ErrInvalidState) || checker.calls != 1 {
		t.Fatalf("retired health probe reached checker: calls=%d err=%v", checker.calls, err)
	}
	replayed, replay, err := service.Create(ctx, ownerA, "create-agent", input)
	if err != nil || !replay || replayed.ID != created.ID || replayed.AggregateVersion != 1 {
		t.Fatalf("create replay: agent=%#v replay=%v err=%v", replayed, replay, err)
	}
	changedCreate := input
	changedCreate.Name = "Different"
	if _, _, err = service.Create(ctx, ownerA, "create-agent", changedCreate); !errors.Is(err, persistence.ErrIdempotencyConflict) {
		t.Fatalf("idempotency key reused with different input: %v", err)
	}
	assertCount(t, db, `SELECT count(*) FROM domain_events WHERE aggregate_id=$1 AND event_type='agent.created'`, created.ID, 1)
	assertCount(t, db, `SELECT count(*) FROM audit_events WHERE resource_id=$1 AND action='agent.created'`, created.ID, 1)
	neverActive, _, err := service.Create(ctx, ownerA, "create-never-active", input)
	if err != nil {
		t.Fatal(err)
	}
	neverActive, _, err = service.Transition(ctx, ownerA, "pause-never-active", neverActive.ID, agent.LifecycleInput{Status: agent.StatusPaused, ExpectedVersion: 1})
	if err != nil || neverActive.ActivatedAt != nil {
		t.Fatalf("pause before activation: agent=%#v err=%v", neverActive, err)
	}
	pausedActions, err := service.AvailableActions(ctx, ownerA, neverActive.ID)
	if err != nil || len(pausedActions.Actions) != 10 || !pausedActions.Actions[9].Allowed {
		t.Fatalf("paused return-to-draft action: %#v err=%v", pausedActions, err)
	}
	editableProfile := profileFrom(neverActive, 2)
	editableProfile.ControllerAddress = "0x3333333333333333333333333333333333333333"
	editableProfile.PayoutAddress = "0x4444444444444444444444444444444444444444"
	neverActive, _, err = service.UpdateProfile(ctx, ownerA, "edit-never-active-addresses", neverActive.ID, editableProfile)
	if err != nil || neverActive.AggregateVersion != 3 || neverActive.ActivatedAt != nil || neverActive.ControllerAddress != editableProfile.ControllerAddress || neverActive.PayoutAddress != editableProfile.PayoutAddress {
		t.Fatalf("never-active paused addresses: agent=%#v err=%v", neverActive, err)
	}

	if _, err = service.Get(ctx, ownerB, created.ID); !errors.Is(err, agent.ErrNotFound) {
		t.Fatalf("other owner read: %v", err)
	}
	if _, _, err = service.UpdateProfile(ctx, ownerB, "foreign-profile", created.ID, profileFrom(created, 1)); !errors.Is(err, agent.ErrNotFound) {
		t.Fatalf("other owner update: %v", err)
	}

	for name, invalid := range map[string]agent.PriceInput{
		"negative":        {OverviewPrice: "-1", FormalPackageGrossPrice: "10", AdditionalVersionPrice: "0", ExternalCostCap: "0", ExpectedVersion: 1},
		"above gross":     {OverviewPrice: "11", FormalPackageGrossPrice: "10", AdditionalVersionPrice: "0", ExternalCostCap: "0", ExpectedVersion: 1},
		"stale aggregate": {OverviewPrice: "1", FormalPackageGrossPrice: "10", AdditionalVersionPrice: "0", ExternalCostCap: "0", ExpectedVersion: 2},
	} {
		t.Run("reject price "+name, func(t *testing.T) {
			_, _, publishErr := service.PublishPrice(ctx, ownerA, "invalid-price-"+name, created.ID, invalid)
			if name == "stale aggregate" {
				if !errors.Is(publishErr, agent.ErrStaleVersion) {
					t.Fatalf("expected stale version, got %v", publishErr)
				}
			} else if !errors.Is(publishErr, agent.ErrInvalidPrice) {
				t.Fatalf("expected invalid price, got %v", publishErr)
			}
		})
	}
	priceInput := agent.PriceInput{OverviewPrice: "5", FormalPackageGrossPrice: "10", AdditionalVersionPrice: "2", ExternalCostCap: "3", ExpectedVersion: 1}
	price, replay, err := service.PublishPrice(ctx, ownerA, "price-1", created.ID, priceInput)
	if err != nil || replay || price.Version != 1 || price.AgentAggregateVersion != 2 || price.IncludedVersions != 3 || price.MaxVersions != 5 {
		t.Fatalf("publish price: price=%#v replay=%v err=%v", price, replay, err)
	}
	priceReplay, replay, err := service.PublishPrice(ctx, ownerA, "price-1", created.ID, priceInput)
	if err != nil || !replay || priceReplay.Version != price.Version || priceReplay.AgentAggregateVersion != price.AgentAggregateVersion {
		t.Fatalf("price replay: price=%#v replay=%v err=%v", priceReplay, replay, err)
	}
	assertCount(t, db, `SELECT count(*) FROM outbox_messages WHERE dedupe_key=$1 AND topic='agent-events' AND payload->>'eventType'='agent.price_published'`, fmt.Sprintf("agent:%s:price:1", created.ID), 1)
	if _, err = db.ExecContext(ctx, `INSERT INTO agent_price_versions (agent_id,version_no,overview_price,formal_package_gross_price,additional_version_price,external_cost_cap,included_versions,max_versions,created_at) VALUES ($1,2,-1,10,0,0,3,5,now())`, created.ID); err == nil {
		t.Fatal("database accepted negative overview price")
	}
	if _, err = db.ExecContext(ctx, `INSERT INTO agent_price_versions (agent_id,version_no,overview_price,formal_package_gross_price,additional_version_price,external_cost_cap,included_versions,max_versions,created_at) VALUES ($1,2,11,10,0,0,3,5,now())`, created.ID); err == nil {
		t.Fatal("database accepted overview price above gross")
	}
	if _, err = db.ExecContext(ctx, `UPDATE agent_price_versions SET overview_price=4 WHERE agent_id=$1 AND version_no=1`, created.ID); err == nil {
		t.Fatal("database allowed immutable price update")
	}
	if _, err = db.ExecContext(ctx, `DELETE FROM agent_price_versions WHERE agent_id=$1 AND version_no=1`, created.ID); err == nil {
		t.Fatal("database allowed immutable price delete")
	}

	if _, _, err = service.Transition(ctx, ownerA, "activate-too-early", created.ID, agent.LifecycleInput{Status: agent.StatusActive, ExpectedVersion: 2}); !errors.Is(err, agent.ErrInvalidState) {
		t.Fatalf("activation without health: %v", err)
	}
	if _, _, err = service.UpdateHealth(ctx, ownerA, "future-health", created.ID, agent.HealthInput{Health: agent.HealthHealthy, ExpectedVersion: 2, CheckedAt: time.Now().UTC().Add(24 * time.Hour)}); !errors.Is(err, agent.ErrInvalidInput) {
		t.Fatalf("future health timestamp: %v", err)
	}
	healthInput := agent.HealthInput{Health: agent.HealthHealthy, ExpectedVersion: 2}
	healthy, replay, err := service.UpdateHealth(ctx, ownerA, "healthy", created.ID, healthInput)
	if err != nil || healthy.AggregateVersion != 3 {
		t.Fatalf("healthy: agent=%#v err=%v", healthy, err)
	}
	healthReplay, replay, err := service.UpdateHealth(ctx, ownerA, "healthy", created.ID, healthInput)
	if err != nil || !replay || healthReplay.AggregateVersion != 3 || healthReplay.HealthCheckedAt == nil || healthy.HealthCheckedAt == nil || !healthReplay.HealthCheckedAt.Equal(*healthy.HealthCheckedAt) {
		t.Fatalf("health replay: agent=%#v replay=%v err=%v", healthReplay, replay, err)
	}
	if _, err = db.ExecContext(ctx, `UPDATE agents SET health_valid_until=clock_timestamp()+interval '150 milliseconds' WHERE agent_id=$1`, created.ID); err != nil {
		t.Fatal(err)
	}
	blocker, err := db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = blocker.ExecContext(ctx, `SELECT agent_id FROM agents WHERE agent_id=$1 FOR UPDATE`, created.ID); err != nil {
		_ = blocker.Rollback()
		t.Fatal(err)
	}
	blockedActivation := make(chan error, 1)
	go func() {
		_, _, transitionErr := service.Transition(ctx, ownerA, "activate-after-lock-wait", created.ID, agent.LifecycleInput{Status: agent.StatusActive, ExpectedVersion: 3})
		blockedActivation <- transitionErr
	}()
	time.Sleep(300 * time.Millisecond)
	if err = blocker.Commit(); err != nil {
		t.Fatal(err)
	}
	if err = <-blockedActivation; !errors.Is(err, agent.ErrInvalidState) {
		t.Fatalf("activation accepted health that expired while waiting for the aggregate lock: %v", err)
	}
	if _, err = db.ExecContext(ctx, `UPDATE agents SET health_valid_until=clock_timestamp()+interval '5 minutes' WHERE agent_id=$1`, created.ID); err != nil {
		t.Fatal(err)
	}
	active, _, err := service.Transition(ctx, ownerA, "activate", created.ID, agent.LifecycleInput{Status: agent.StatusActive, ExpectedVersion: 3})
	if err != nil || active.AggregateVersion != 4 || active.ActivatedAt == nil {
		t.Fatalf("activate: agent=%#v err=%v", active, err)
	}
	activatedAt := *active.ActivatedAt
	paused, _, err := service.Transition(ctx, ownerA, "pause", created.ID, agent.LifecycleInput{Status: agent.StatusPaused, ExpectedVersion: 4})
	if err != nil || paused.AggregateVersion != 5 || paused.ActivatedAt == nil || !paused.ActivatedAt.Equal(activatedAt) {
		t.Fatalf("pause: agent=%#v err=%v", paused, err)
	}
	changedProfile := profileFrom(paused, 5)
	changedProfile.ControllerAddress = "0x3333333333333333333333333333333333333333"
	changedProfile.Name = "Must Roll Back"
	if _, _, err = service.UpdateProfile(ctx, ownerA, "frozen-address", created.ID, changedProfile); !errors.Is(err, agent.ErrInvalidState) {
		t.Fatalf("address changed after activation: %v", err)
	}
	afterRejected, err := service.Get(ctx, ownerA, created.ID)
	if err != nil || afterRejected.AggregateVersion != 5 || afterRejected.Name != created.Name || afterRejected.ControllerAddress != created.ControllerAddress {
		t.Fatalf("frozen update had partial effect: agent=%#v err=%v", afterRejected, err)
	}
	if _, err = db.ExecContext(ctx, `UPDATE agents SET payout_address='0x3333333333333333333333333333333333333333' WHERE agent_id=$1`, created.ID); err == nil {
		t.Fatal("database allowed activated payout address mutation")
	}
	metadataProfile := profileFrom(paused, 5)
	metadataProfile.Name = "Updated Metadata"
	updated, _, err := service.UpdateProfile(ctx, ownerA, "metadata-after-pause", created.ID, metadataProfile)
	if err != nil || updated.AggregateVersion != 6 || updated.Name != "Updated Metadata" {
		t.Fatalf("non-address profile update after pause: agent=%#v err=%v", updated, err)
	}
	if _, _, err = service.UpdateHealth(ctx, ownerA, "stale-health", created.ID, agent.HealthInput{Health: agent.HealthHealthy, ExpectedVersion: 5, CheckedAt: time.Now().UTC()}); !errors.Is(err, agent.ErrStaleVersion) {
		t.Fatalf("stale aggregate update: %v", err)
	}
	active, _, err = service.Transition(ctx, ownerA, "reactivate", created.ID, agent.LifecycleInput{Status: agent.StatusActive, ExpectedVersion: 6})
	if err != nil || active.AggregateVersion != 7 || active.ActivatedAt == nil || !active.ActivatedAt.Equal(activatedAt) {
		t.Fatalf("reactivate: agent=%#v err=%v", active, err)
	}

	type reservationResult struct {
		lease agent.CapacityLease
		err   error
	}
	results := make(chan reservationResult, 6)
	var group sync.WaitGroup
	for index := 0; index < 6; index++ {
		group.Add(1)
		go func(index int) {
			defer group.Done()
			lease, reserveErr := service.ReserveCapacity(ctx, created.ID, fmt.Sprintf("reservation-%d", index), time.Minute)
			results <- reservationResult{lease: lease, err: reserveErr}
		}(index)
	}
	group.Wait()
	close(results)
	var leases []agent.CapacityLease
	for result := range results {
		if result.err == nil {
			leases = append(leases, result.lease)
		} else if !errors.Is(result.err, agent.ErrCapacityUnavailable) {
			t.Fatalf("reserve capacity: %v", result.err)
		}
	}
	if len(leases) != input.MaxConcurrency {
		t.Fatalf("capacity oversubscribed or undersubscribed: got %d leases, want %d", len(leases), input.MaxConcurrency)
	}
	capacityView, err := service.View(ctx, ownerA, created.ID)
	if err != nil || capacityView.Agent.ActiveCapacity != len(leases) || capacityView.AvailableActions.Actions[8].Allowed || len(capacityView.AvailableActions.Actions[8].Reasons) != 1 || capacityView.AvailableActions.Actions[8].Reasons[0].Code != "active_capacity_nonzero" {
		t.Fatalf("lease-consistent Agent view: %#v err=%v", capacityView, err)
	}
	shrinkProfile := profileFrom(active, 7)
	shrinkProfile.MaxConcurrency = 1
	if _, _, err = service.UpdateProfile(ctx, ownerA, "shrink-profile", created.ID, shrinkProfile); !errors.Is(err, agent.ErrInvalidState) {
		t.Fatalf("profile shrank below active capacity: %v", err)
	}
	expanded, _, err := service.UpdateCapacity(ctx, ownerA, "expand-capacity", created.ID, agent.CapacityInput{MaxConcurrency: 3, ExpectedVersion: 7})
	if err != nil || expanded.AggregateVersion != 8 || expanded.MaxConcurrency != 3 || expanded.ActiveCapacity != 2 {
		t.Fatalf("expand capacity: agent=%#v err=%v", expanded, err)
	}
	replayedLease, err := service.ReserveCapacity(ctx, created.ID, leases[0].ReservationID, time.Minute)
	if err != nil || replayedLease.FencingToken != leases[0].FencingToken || !replayedLease.ExpiresAt.Equal(leases[0].ExpiresAt) {
		t.Fatalf("reservation replay changed result: original=%#v replay=%#v err=%v", leases[0], replayedLease, err)
	}
	if _, _, err = service.Transition(ctx, ownerA, "retire-with-capacity", created.ID, agent.LifecycleInput{Status: agent.StatusRetired, ExpectedVersion: 8}); !errors.Is(err, agent.ErrInvalidState) {
		t.Fatalf("retired with active capacity: %v", err)
	}
	if err = service.ReleaseCapacity(ctx, leases[0].ReservationID, leases[0].FencingToken+1); !errors.Is(err, agent.ErrStaleVersion) {
		t.Fatalf("stale fencing token: %v", err)
	}
	maxToken := int64(0)
	for _, lease := range leases {
		if lease.FencingToken > maxToken {
			maxToken = lease.FencingToken
		}
		if err = service.ReleaseCapacity(ctx, lease.ReservationID, lease.FencingToken); err != nil {
			t.Fatalf("release capacity: %v", err)
		}
		if err = service.ReleaseCapacity(ctx, lease.ReservationID, lease.FencingToken); err != nil {
			t.Fatalf("release replay: %v", err)
		}
	}
	duplicateResults := make(chan reservationResult, 2)
	for range 2 {
		group.Add(1)
		go func() {
			defer group.Done()
			lease, reserveErr := service.ReserveCapacity(ctx, created.ID, "reservation-duplicate", time.Minute)
			duplicateResults <- reservationResult{lease: lease, err: reserveErr}
		}()
	}
	group.Wait()
	close(duplicateResults)
	var duplicateLease agent.CapacityLease
	for result := range duplicateResults {
		if result.err != nil {
			t.Fatalf("concurrent reservation replay: %v", result.err)
		}
		if duplicateLease.ReservationID == "" {
			duplicateLease = result.lease
		} else if result.lease.FencingToken != duplicateLease.FencingToken || !result.lease.ExpiresAt.Equal(duplicateLease.ExpiresAt) {
			t.Fatalf("concurrent reservation replay changed result: first=%#v second=%#v", duplicateLease, result.lease)
		}
	}
	if duplicateLease.FencingToken <= maxToken {
		t.Fatalf("duplicate reservation fencing token was not monotonic: previous=%d duplicate=%#v", maxToken, duplicateLease)
	}
	maxToken = duplicateLease.FencingToken
	if err = service.ReleaseCapacity(ctx, duplicateLease.ReservationID, duplicateLease.FencingToken); err != nil {
		t.Fatal(err)
	}
	nextLease, err := service.ReserveCapacity(ctx, created.ID, "reservation-next", time.Minute)
	if err != nil || nextLease.FencingToken <= maxToken {
		t.Fatalf("fencing token was not monotonic: previous=%d next=%#v err=%v", maxToken, nextLease, err)
	}
	if err = service.ReleaseCapacity(ctx, nextLease.ReservationID, nextLease.FencingToken); err != nil {
		t.Fatal(err)
	}
	expiredLease, err := service.ReserveCapacity(ctx, created.ID, "reservation-expired", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.ExecContext(ctx, `UPDATE agent_capacity_leases SET expires_at=now()-interval '1 second' WHERE reservation_id=$1`, expiredLease.ReservationID); err != nil {
		t.Fatal(err)
	}
	freshRead, err := service.Get(ctx, ownerA, created.ID)
	if err != nil || freshRead.ActiveCapacity != 0 {
		t.Fatalf("read returned stale active capacity after lease expiry: agent=%#v err=%v", freshRead, err)
	}
	retired, _, err := service.Transition(ctx, ownerA, "retire", created.ID, agent.LifecycleInput{Status: agent.StatusRetired, ExpectedVersion: 8})
	if err != nil || retired.AggregateVersion != 9 || retired.Status != agent.StatusRetired {
		t.Fatalf("retire: agent=%#v err=%v", retired, err)
	}
	if _, _, err = service.UpdateHealth(ctx, ownerA, "retired-health", created.ID, agent.HealthInput{Health: agent.HealthHealthy, ExpectedVersion: 9, CheckedAt: time.Now().UTC()}); !errors.Is(err, agent.ErrInvalidState) {
		t.Fatalf("retired agent changed: %v", err)
	}
	endpointAgent, _, err := service.Create(ctx, ownerA, "create-endpoint-agent", input)
	if err != nil {
		t.Fatal(err)
	}
	endpointAgent, _, err = service.UpdateHealth(ctx, ownerA, "endpoint-agent-health", endpointAgent.ID, agent.HealthInput{Health: agent.HealthHealthy, ExpectedVersion: 1})
	if err != nil || endpointAgent.Health != agent.HealthHealthy {
		t.Fatalf("endpoint Agent health setup: agent=%#v err=%v", endpointAgent, err)
	}
	endpointProfile := profileFrom(endpointAgent, 2)
	endpointProfile.EndpointURL = "https://replacement-agent.example"
	endpointAgent, _, err = service.UpdateProfile(ctx, ownerA, "change-endpoint", endpointAgent.ID, endpointProfile)
	if err != nil || endpointAgent.Health != agent.HealthUnknown || endpointAgent.HealthCheckedAt != nil || endpointAgent.HealthValidUntil != nil {
		t.Fatalf("endpoint change did not invalidate health: agent=%#v err=%v", endpointAgent, err)
	}
	assertCount(t, db, `SELECT count(*) FROM domain_events WHERE aggregate_id=$1`, created.ID, 9)
	assertCount(t, db, `SELECT count(*) FROM audit_events WHERE resource_id=$1`, created.ID, 9)
	assertCount(t, db, `SELECT count(*) FROM information_schema.columns WHERE table_schema=current_schema() AND table_name IN ('agents','agent_price_versions','agent_capacity_leases') AND (column_name LIKE '%bond%' OR column_name LIKE '%deposit%')`, nil, 0)
}

func integrationCreateInput() agent.CreateInput {
	return agent.CreateInput{
		Name:                     "Research Agent",
		Category:                 "research",
		Tags:                     []string{"analysis"},
		Capabilities:             "Produces structured research",
		Languages:                []string{"zh-CN", "en"},
		EstimatedDurationSeconds: 300,
		AuthorBio:                "Provider",
		EndpointURL:              "https://agent.example",
		ControllerAddress:        "0x1111111111111111111111111111111111111111",
		PayoutAddress:            "0x2222222222222222222222222222222222222222",
		MaxConcurrency:           2,
	}
}

func profileFrom(value agent.Agent, expectedVersion int64) agent.ProfileInput {
	return agent.ProfileInput{CreateInput: agent.CreateInput{
		Name:                     value.Name,
		Category:                 value.Category,
		Tags:                     value.Tags,
		Capabilities:             value.Capabilities,
		Languages:                value.Languages,
		EstimatedDurationSeconds: value.EstimatedDurationSeconds,
		AuthorBio:                value.AuthorBio,
		EndpointURL:              value.EndpointURL,
		ControllerAddress:        value.ControllerAddress,
		PayoutAddress:            value.PayoutAddress,
		MaxConcurrency:           value.MaxConcurrency,
	}, ExpectedVersion: expectedVersion}
}

func assertCount(t *testing.T, db *sql.DB, query string, argument any, expected int) {
	t.Helper()
	var actual int
	var err error
	if argument == nil {
		err = db.QueryRow(query).Scan(&actual)
	} else {
		err = db.QueryRow(query, argument).Scan(&actual)
	}
	if err != nil {
		t.Fatal(err)
	}
	if actual != expected {
		t.Fatalf("query %q: expected %d, got %d", query, expected, actual)
	}
}

func agentSearchPath(databaseURL, schema string) string {
	parsed, err := url.Parse(databaseURL)
	if err == nil && parsed.Scheme != "" {
		query := parsed.Query()
		query.Set("search_path", schema)
		parsed.RawQuery = query.Encode()
		return parsed.String()
	}
	separator := "?"
	if strings.Contains(databaseURL, "?") {
		separator = "&"
	}
	return databaseURL + separator + "search_path=" + url.QueryEscape(schema)
}
