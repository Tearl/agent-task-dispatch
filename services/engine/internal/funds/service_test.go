package funds

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"sync"
	"testing"
	"time"

	"github.com/example/agent-platform/engine/internal/overview"
)

func TestIsolatedAccountsBalancedCaptureAndReplay(t *testing.T) {
	service, repository, discovery, formal := fundedService(t, "100", "200")
	allocation, replay, err := service.AuthorizeOverview(context.Background(), authorization("allocation-key-1", "agent-1", "20", "10"))
	if err != nil || replay || allocation.AccountID != discovery.ID || allocation.ReserveAmount != "30" {
		t.Fatalf("authorize: allocation=%#v replay=%v err=%v", allocation, replay, err)
	}
	claim := captureClaim("agent-1", "20", "7")
	captured, replay, err := service.CaptureOverview(context.Background(), allocation.ID, claim)
	if err != nil || replay || captured.Status != AllocationCaptured || captured.CapturedOverview != "20" || captured.CapturedCost != "7" || captured.CaptureJournalID == "" {
		t.Fatalf("capture: allocation=%#v replay=%v err=%v", captured, replay, err)
	}
	_, replay, err = service.CaptureOverview(context.Background(), allocation.ID, claim)
	if err != nil || !replay {
		t.Fatalf("capture replay: replay=%v err=%v", replay, err)
	}
	discoveryAfter, _ := repository.GetAccount(context.Background(), discovery.ID)
	formalAfter, _ := repository.GetAccount(context.Background(), formal.ID)
	if discoveryAfter.Balance != "73" || formalAfter.Balance != "200" {
		t.Fatalf("cross-account spend: discovery=%s formal=%s", discoveryAfter.Balance, formalAfter.Balance)
	}
	journal, err := repository.GetJournal(context.Background(), captured.CaptureJournalID)
	if err != nil || sum(journal.Entries, EntryDebit) != "27" || sum(journal.Entries, EntryCredit) != "27" {
		t.Fatalf("unbalanced capture journal: %#v err=%v", journal, err)
	}
	changed := claim
	changed.UsedCost = "8"
	if _, _, err = service.CaptureOverview(context.Background(), allocation.ID, changed); !errors.Is(err, ErrContentConflict) {
		t.Fatalf("changed capture replay accepted: %v", err)
	}
}

func TestAuthorizationRetryKeepsEarlierAuthorizedDeadline(t *testing.T) {
	service, repository, _, _ := fundedService(t, "100", "0")
	request := authorization("retry-key", "agent-retry", "20", "10")
	created, replay, err := service.AuthorizeOverview(context.Background(), request)
	if err != nil || replay {
		t.Fatalf("initial authorization: replay=%v err=%v", replay, err)
	}

	retry := request
	retry.Deadline = request.Deadline.Add(15 * time.Minute)
	replayed, replay, err := service.AuthorizeOverview(context.Background(), retry)
	if err != nil || !replay || !replayed.Deadline.Equal(created.Deadline) || len(repository.allocations) != 1 {
		t.Fatalf("later-deadline retry: allocation=%#v replay=%v count=%d err=%v", replayed, replay, len(repository.allocations), err)
	}

	earlier := request
	earlier.Deadline = request.Deadline.Add(-15 * time.Minute)
	if _, _, err = service.AuthorizeOverview(context.Background(), earlier); !errors.Is(err, ErrContentConflict) {
		t.Fatalf("earlier-deadline retry accepted: %v", err)
	}
	changed := retry
	changed.OverviewPrice = "21"
	if _, _, err = service.AuthorizeOverview(context.Background(), changed); !errors.Is(err, ErrContentConflict) {
		t.Fatalf("changed authorization retry accepted: %v", err)
	}
}

func TestAllocationReservationIsAtomicAndCannotBeReused(t *testing.T) {
	service, _, _, _ := fundedService(t, "50", "0")
	requests := []OverviewAuthorization{authorization("key-a", "agent-a", "20", "10"), authorization("key-b", "agent-b", "20", "10")}
	var group sync.WaitGroup
	errorsSeen := make(chan error, len(requests))
	for _, request := range requests {
		group.Add(1)
		go func(request OverviewAuthorization) {
			defer group.Done()
			_, _, err := service.AuthorizeOverview(context.Background(), request)
			errorsSeen <- err
		}(request)
	}
	group.Wait()
	close(errorsSeen)
	successes, insufficient := 0, 0
	for err := range errorsSeen {
		switch {
		case err == nil:
			successes++
		case errors.Is(err, ErrInsufficient):
			insufficient++
		default:
			t.Fatalf("unexpected authorization error: %v", err)
		}
	}
	if successes != 1 || insufficient != 1 {
		t.Fatalf("reservation race: success=%d insufficient=%d", successes, insufficient)
	}
	allocation, _, err := service.AuthorizeOverview(context.Background(), authorization("release-key", "agent-release", "10", "0"))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = service.ReleaseOverview(context.Background(), allocation.ID, "overview_invalid"); err != nil {
		t.Fatal(err)
	}
	if _, replay, err := service.ReleaseOverview(context.Background(), allocation.ID, "overview_invalid"); err != nil || !replay {
		t.Fatalf("release replay: replay=%v err=%v", replay, err)
	}
	if _, _, err = service.ReleaseOverview(context.Background(), allocation.ID, "different_reason"); !errors.Is(err, ErrContentConflict) {
		t.Fatalf("changed release accepted: %v", err)
	}
	if _, _, err = service.CaptureOverview(context.Background(), allocation.ID, captureClaim("agent-release", "10", "0")); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("released allocation captured: %v", err)
	}
}

func TestJournalCorrectionUsesSingleBalancedReversal(t *testing.T) {
	service, repository, discovery, _ := fundedService(t, "40", "0")
	funding, err := findJournal(repository, "fund-discovery")
	if err != nil {
		t.Fatal(err)
	}
	reversal, replay, err := service.ReverseJournal(context.Background(), ReverseRequest{IdempotencyKey: "reverse-funding", JournalID: funding.ID, ReasonCode: "chain_event_reverted"})
	if err != nil || replay || reversal.ReversalOf != funding.ID || sum(reversal.Entries, EntryDebit) != sum(reversal.Entries, EntryCredit) {
		t.Fatalf("reversal: %#v replay=%v err=%v", reversal, replay, err)
	}
	account, _ := repository.GetAccount(context.Background(), discovery.ID)
	if account.Balance != "0" {
		t.Fatalf("reversal did not restore balance: %s", account.Balance)
	}
	if _, replay, err = service.ReverseJournal(context.Background(), ReverseRequest{IdempotencyKey: "reverse-funding", JournalID: funding.ID, ReasonCode: "chain_event_reverted"}); err != nil || !replay {
		t.Fatalf("reversal replay: replay=%v err=%v", replay, err)
	}
	if _, _, err = service.ReverseJournal(context.Background(), ReverseRequest{IdempotencyKey: "another-reversal", JournalID: funding.ID, ReasonCode: "manual_fix"}); !errors.Is(err, ErrContentConflict) {
		t.Fatalf("second reversal accepted: %v", err)
	}
}

func TestOverviewGatewayImplementsFrozenPort(t *testing.T) {
	service, _, _, _ := fundedService(t, "100", "0")
	gateway, err := NewOverviewGateway(service)
	if err != nil {
		t.Fatal(err)
	}
	request := authorization("gateway-key", "agent-gateway", "11", "4")
	allocated, replay, err := gateway.AuthorizeOverview(context.Background(), overview.AllocationRequest{IdempotencyKey: request.IdempotencyKey, TaskID: request.TaskID, TaskSpecHash: request.TaskSpecHash, SnapshotID: request.SnapshotID, MatchRevision: request.MatchRevision, AgentID: request.AgentID, PriceVersion: request.PriceVersion, QuoteHash: request.QuoteHash, OverviewPrice: request.OverviewPrice, ExternalCostCap: request.ExternalCostCap, Deadline: request.Deadline})
	if err != nil || replay || allocated.ID == "" || allocated.CostCap != "4" {
		t.Fatalf("gateway authorize: %#v replay=%v err=%v", allocated, replay, err)
	}
	claim := overview.BillingClaim{TaskID: request.TaskID, TaskSpecHash: request.TaskSpecHash, MatchRevision: request.MatchRevision, LogicalExecutionID: digest("gateway-execution"), AgentID: request.AgentID, QuoteHash: request.QuoteHash, ContentHash: digest("gateway-content"), Amount: "11", UsedCost: "3"}
	if replay, err = gateway.CaptureOverview(context.Background(), allocated.ID, claim); err != nil || replay {
		t.Fatalf("gateway capture: replay=%v err=%v", replay, err)
	}
	if replay, err = gateway.CaptureOverview(context.Background(), allocated.ID, claim); err != nil || !replay {
		t.Fatalf("gateway capture replay: replay=%v err=%v", replay, err)
	}
	releaseRequest := authorization("gateway-release-key", "agent-release", "5", "1")
	releasable, _, err := gateway.AuthorizeOverview(context.Background(), overview.AllocationRequest{IdempotencyKey: releaseRequest.IdempotencyKey, TaskID: releaseRequest.TaskID, TaskSpecHash: releaseRequest.TaskSpecHash, SnapshotID: releaseRequest.SnapshotID, MatchRevision: releaseRequest.MatchRevision, AgentID: releaseRequest.AgentID, PriceVersion: releaseRequest.PriceVersion, QuoteHash: releaseRequest.QuoteHash, OverviewPrice: releaseRequest.OverviewPrice, ExternalCostCap: releaseRequest.ExternalCostCap, Deadline: releaseRequest.Deadline})
	if err != nil {
		t.Fatal(err)
	}
	if replay, err = gateway.ReleaseOverview(context.Background(), releasable.ID, "overview_invalid"); err != nil || replay {
		t.Fatalf("gateway release: replay=%v err=%v", replay, err)
	}
}

func TestAllocationPropertyPreservesValueAndIsolation(t *testing.T) {
	service, repository, discovery, formal := fundedService(t, "1000000", "500000")
	random := rand.New(rand.NewSource(401))
	capturedTotal := "0"
	for index := 0; index < 200; index++ {
		agent := fmt.Sprintf("property-agent-%03d", index)
		price := fmt.Sprintf("%d", random.Intn(100)+1)
		cap := fmt.Sprintf("%d", random.Intn(30))
		allocation, _, err := service.AuthorizeOverview(context.Background(), authorization(fmt.Sprintf("property-key-%03d", index), agent, price, cap))
		if err != nil {
			t.Fatal(err)
		}
		if random.Intn(3) == 0 {
			if _, _, err = service.ReleaseOverview(context.Background(), allocation.ID, "property_release"); err != nil {
				t.Fatal(err)
			}
			continue
		}
		used := fmt.Sprintf("%d", random.Intn(mustInt(cap)+1))
		captured, _, err := service.CaptureOverview(context.Background(), allocation.ID, captureClaim(agent, price, used))
		if err != nil {
			t.Fatal(err)
		}
		capturedTotal = addMoney(capturedTotal, addMoney(price, used))
		journal, err := repository.GetJournal(context.Background(), captured.CaptureJournalID)
		if err != nil || sum(journal.Entries, EntryDebit) != sum(journal.Entries, EntryCredit) {
			t.Fatalf("iteration %d produced unbalanced journal: %#v err=%v", index, journal, err)
		}
	}
	discoveryAfter, _ := repository.GetAccount(context.Background(), discovery.ID)
	formalAfter, _ := repository.GetAccount(context.Background(), formal.ID)
	if addMoney(discoveryAfter.Balance, capturedTotal) != "1000000" || formalAfter.Balance != "500000" {
		t.Fatalf("value/isolation invariant failed: discovery=%s captured=%s formal=%s", discoveryAfter.Balance, capturedTotal, formalAfter.Balance)
	}
}

func fundedService(t *testing.T, discoveryAmount, formalAmount string) (*Service, *MemoryRepository, Account, Account) {
	t.Helper()
	repository := NewMemoryRepository()
	service, err := NewService(repository, "eip155:31337/native:18")
	if err != nil {
		t.Fatal(err)
	}
	service.now = func() time.Time { return time.Date(2026, 8, 22, 12, 0, 0, 0, time.UTC) }
	discovery, _, err := service.OpenAccount(context.Background(), OpenAccountRequest{Type: AccountDiscoveryPool, TaskID: "task-1", ReferenceID: "task-1", Asset: "eip155:31337/native:18", PrincipalOwnerID: "publisher-1", ResidualRecipientID: "publisher-1", RefundPolicyVersion: "refund-v1"})
	if err != nil {
		t.Fatal(err)
	}
	formal, _, err := service.OpenAccount(context.Background(), OpenAccountRequest{Type: AccountFormalEscrow, TaskID: "task-1", ReferenceID: "task-1", Asset: "eip155:31337/native:18", PrincipalOwnerID: "publisher-1", ResidualRecipientID: "publisher-1", RefundPolicyVersion: "refund-v1"})
	if err != nil {
		t.Fatal(err)
	}
	if discoveryAmount != "0" {
		if _, _, err = service.RecordFunding(context.Background(), FundingRequest{IdempotencyKey: "fund-discovery", AccountID: discovery.ID, Amount: discoveryAmount, ExternalRef: "chain:1/log:1"}); err != nil {
			t.Fatal(err)
		}
	}
	if formalAmount != "0" {
		if _, _, err = service.RecordFunding(context.Background(), FundingRequest{IdempotencyKey: "fund-formal", AccountID: formal.ID, Amount: formalAmount, ExternalRef: "chain:1/log:2"}); err != nil {
			t.Fatal(err)
		}
	}
	return service, repository, discovery, formal
}

func authorization(key, agent, overviewPrice, costCap string) OverviewAuthorization {
	return OverviewAuthorization{IdempotencyKey: key, TaskID: "task-1", TaskSpecHash: digest("spec"), SnapshotID: digest("snapshot"), MatchRevision: 1, AgentID: agent, PriceVersion: 1, QuoteHash: digest("quote:" + agent), OverviewPrice: overviewPrice, ExternalCostCap: costCap, Deadline: time.Date(2026, 8, 22, 13, 0, 0, 0, time.UTC)}
}

func captureClaim(agent, amount, cost string) OverviewCapture {
	return OverviewCapture{TaskID: "task-1", TaskSpecHash: digest("spec"), MatchRevision: 1, LogicalExecutionID: digest("execution:" + agent), AgentID: agent, QuoteHash: digest("quote:" + agent), ContentHash: digest("content:" + agent), OverviewAmount: amount, UsedCost: cost}
}

func digest(value string) string { return hashJSON(value) }

func sum(entries []Entry, direction string) string {
	total := "0"
	for _, entry := range entries {
		if entry.Direction == direction {
			total = addMoney(total, entry.Amount)
		}
	}
	return total
}

func findJournal(repository *MemoryRepository, key string) (Journal, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	id, ok := repository.journalKeys[key]
	if !ok {
		return Journal{}, ErrNotFound
	}
	return cloneJournal(repository.journals[id]), nil
}

func mustInt(value string) int {
	parsed := 0
	for _, digit := range value {
		parsed = parsed*10 + int(digit-'0')
	}
	return parsed
}
