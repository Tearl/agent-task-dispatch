package workflow

import (
	"testing"
	"time"

	"github.com/example/agent-platform/engine/internal/matching"
)

func TestEffectiveMatchingHashIgnoresCandidateOrdering(t *testing.T) {
	request, candidates := effectiveHashFixture()
	first, err := effectiveMatchingHash(request, candidates)
	if err != nil {
		t.Fatal(err)
	}
	candidates[0], candidates[1] = candidates[1], candidates[0]
	candidates[0].Tags[0], candidates[0].Tags[1] = candidates[0].Tags[1], candidates[0].Tags[0]
	second, err := effectiveMatchingHash(request, candidates)
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatalf("non-business ordering or trigger time changed hash: %s != %s", first, second)
	}
}

func TestEffectiveMatchingHashChangesAcrossEligibilityTime(t *testing.T) {
	request, candidates := effectiveHashFixture()
	first, err := effectiveMatchingHash(request, candidates)
	if err != nil {
		t.Fatal(err)
	}
	request.Now = request.Now.Add(time.Nanosecond)
	second, err := effectiveMatchingHash(request, candidates)
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("time-dependent eligibility/scoring input did not change snapshot identity")
	}
}

func TestEffectiveMatchingHashChangesWithAuthoritativeBusinessInput(t *testing.T) {
	request, candidates := effectiveHashFixture()
	base, err := effectiveMatchingHash(request, candidates)
	if err != nil {
		t.Fatal(err)
	}
	changes := []func([]matching.Candidate){
		func(values []matching.Candidate) { values[0].RiskStatus = "blocked" },
		func(values []matching.Candidate) { values[0].Reputation.Quality-- },
		func(values []matching.Candidate) { values[0].VectorVersion = "vector-v2" },
		func(values []matching.Candidate) { values[0].ActiveCapacity++ },
		func(values []matching.Candidate) { values[0].FormalPrice = "501" },
	}
	for index, change := range changes {
		_, fresh := effectiveHashFixture()
		change(fresh)
		changed, hashErr := effectiveMatchingHash(request, fresh)
		if hashErr != nil {
			t.Fatal(hashErr)
		}
		if changed == base {
			t.Fatalf("authoritative change %d did not change effective hash", index)
		}
	}
}

func effectiveHashFixture() (matching.Request, []matching.Candidate) {
	now := time.Date(2026, 8, 28, 10, 0, 0, 0, time.UTC)
	request := matching.Request{TaskID: "task-1", PublisherID: "publisher-1", Category: "research", Language: "zh-CN", Terms: []string{"report", "market"}, Tags: []string{"market", "report"}, RequiredCapabilities: []string{"report", "research"}, RequiredProtocolVersion: "agent-execution-v1", RequiredVectorVersion: vectorVersion, OverviewBudget: "100", FormalBudget: "1000", ExternalCostCap: "50", Deadline: now.Add(time.Hour), Now: now}
	base := matching.Candidate{ProviderID: "provider", Status: "active", ApprovalStatus: "approved", Health: "healthy", MaxConcurrency: 2, Category: "research", Languages: []string{"zh-CN", "en"}, Tags: []string{"market", "report"}, Capabilities: []string{"research", "report"}, ProtocolVersion: "agent-execution-v1", RiskStatus: "eligible", PayoutAddress: "0x1111111111111111111111111111111111111111", VectorVersion: vectorVersion, EstimatedDuration: time.Minute, OverviewPrice: "50", FormalPrice: "500", ExternalCostCap: "25", PriceVersion: 1, ReputationAvailable: true, Reputation: matching.Reputation{Quality: 80, Speed: 80, Reliability: 80, Communication: 80, Compliance: 80}}
	first, second := base, base
	first.AgentID = "agent-a"
	second.AgentID = "agent-b"
	return request, []matching.Candidate{first, second}
}
