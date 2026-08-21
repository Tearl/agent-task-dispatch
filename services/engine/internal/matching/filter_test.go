package matching

import (
	"testing"
	"time"
)

func TestHardFilterCoversEveryEligibilityGate(t *testing.T) {
	request, candidate := validFixture()
	tests := []struct {
		name  string
		code  string
		alter func(*Candidate)
	}{
		{name: "identity", code: "agent_identity_invalid", alter: func(value *Candidate) { value.AgentID = "" }},
		{name: "self provider", code: "self_provider", alter: func(value *Candidate) { value.ProviderID = request.PublisherID }},
		{name: "status", code: "status_not_active", alter: func(value *Candidate) { value.Status = "paused" }},
		{name: "approval", code: "approval_required", alter: func(value *Candidate) { value.ApprovalStatus = "pending" }},
		{name: "health", code: "health_not_healthy", alter: func(value *Candidate) { value.Health = "degraded" }},
		{name: "health freshness", code: "health_check_stale", alter: func(value *Candidate) {
			checkedAt := request.Now.Add(-healthFreshness - time.Second)
			value.HealthCheckedAt = &checkedAt
		}},
		{name: "capacity", code: "capacity_unavailable", alter: func(value *Candidate) { value.ActiveCapacity = value.MaxConcurrency }},
		{name: "category", code: "category_mismatch", alter: func(value *Candidate) { value.Category = "translation" }},
		{name: "protocol", code: "protocol_mismatch", alter: func(value *Candidate) { value.ProtocolVersion = "v0" }},
		{name: "language", code: "language_mismatch", alter: func(value *Candidate) { value.Languages = []string{"en"} }},
		{name: "capability", code: "capability_missing", alter: func(value *Candidate) { value.Capabilities = []string{"research"} }},
		{name: "risk", code: "risk_not_eligible", alter: func(value *Candidate) { value.RiskStatus = "blocked" }},
		{name: "payout", code: "payout_address_invalid", alter: func(value *Candidate) { value.PayoutAddress = "invalid" }},
		{name: "vector", code: "vector_version_mismatch", alter: func(value *Candidate) { value.VectorVersion = "old" }},
		{name: "deadline", code: "deadline_unavailable", alter: func(value *Candidate) { value.EstimatedDuration = 25 * time.Hour }},
		{name: "overview budget", code: "overview_budget_exceeded", alter: func(value *Candidate) { value.OverviewPrice = "101" }},
		{name: "formal budget", code: "formal_budget_exceeded", alter: func(value *Candidate) { value.FormalPrice = "1001" }},
		{name: "external cost", code: "external_cost_exceeded", alter: func(value *Candidate) { value.ExternalCostCap = "51" }},
		{name: "price encoding", code: "price_invalid", alter: func(value *Candidate) { value.FormalPrice = "01" }},
		{name: "signed price", code: "price_invalid", alter: func(value *Candidate) { value.FormalPrice = "+1" }},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			changed := candidate
			test.alter(&changed)
			eligible, excluded := HardFilter(request, []Candidate{changed})
			if len(eligible) != 0 || len(excluded) != 1 || !hasReason(excluded[0], test.code) {
				t.Fatalf("expected exclusion %q, eligible=%#v excluded=%#v", test.code, eligible, excluded)
			}
		})
	}
}

func TestHardFilterAcceptsDeadlineBoundaryAndSortsDeterministically(t *testing.T) {
	request, first := validFixture()
	first.AgentID = "agent-b"
	first.EstimatedDuration = request.Deadline.Sub(request.Now)
	second := first
	second.AgentID = "agent-a"

	eligible, excluded := HardFilter(request, []Candidate{first, second})
	if len(excluded) != 0 || len(eligible) != 2 || eligible[0].AgentID != "agent-a" || eligible[1].AgentID != "agent-b" {
		t.Fatalf("unexpected deterministic filter result: eligible=%#v excluded=%#v", eligible, excluded)
	}
}

func hasReason(exclusion Exclusion, code string) bool {
	for _, reason := range exclusion.Reasons {
		if reason.Code == code {
			return true
		}
	}
	return false
}

func validFixture() (Request, Candidate) {
	now := time.Date(2026, 8, 21, 10, 0, 0, 0, time.UTC)
	checkedAt := now.Add(-time.Minute)
	validUntil := now.Add(time.Minute)
	request := Request{
		TaskID:                  "task-1",
		PublisherID:             "publisher-1",
		Category:                "research",
		Language:                "zh-CN",
		Terms:                   []string{"market", "report"},
		RequiredCapabilities:    []string{"research", "report"},
		RequiredProtocolVersion: "agent-v1",
		RequiredVectorVersion:   "embedding-v1",
		OverviewBudget:          "100",
		FormalBudget:            "1000",
		ExternalCostCap:         "50",
		Deadline:                now.Add(24 * time.Hour),
		Now:                     now,
	}
	candidate := Candidate{
		AgentID:           "agent-1",
		ProviderID:        "provider-1",
		Status:            "active",
		ApprovalStatus:    "approved",
		Health:            "healthy",
		HealthCheckedAt:   &checkedAt,
		HealthValidUntil:  &validUntil,
		MaxConcurrency:    4,
		ActiveCapacity:    0,
		Category:          "research",
		Languages:         []string{"zh-CN", "en"},
		Tags:              []string{"market", "report"},
		Capabilities:      []string{"research", "report"},
		ProtocolVersion:   "agent-v1",
		RiskStatus:        "eligible",
		PayoutAddress:     "0x1111111111111111111111111111111111111111",
		VectorVersion:     "embedding-v1",
		EstimatedDuration: time.Hour,
		OverviewPrice:     "50",
		FormalPrice:       "500",
		ExternalCostCap:   "25",
		Reputation: Reputation{
			Quality:       100,
			Speed:         100,
			Reliability:   100,
			Communication: 100,
			Compliance:    100,
		},
	}
	return request, candidate
}
