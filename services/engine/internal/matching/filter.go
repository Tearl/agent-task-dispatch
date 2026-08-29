package matching

import (
	"math/big"
	"slices"
	"strings"
	"time"
)

const healthFreshness = 5 * time.Minute

var reasonMessages = map[string]string{
	"self_provider":            "A provider cannot match its own task.",
	"agent_identity_invalid":   "Agent identity is invalid.",
	"status_not_active":        "Agent is not active.",
	"approval_required":        "Agent approval is not current.",
	"health_not_healthy":       "Agent health is not healthy.",
	"health_check_stale":       "Agent health check is stale.",
	"capacity_unavailable":     "Agent has no remaining capacity.",
	"category_mismatch":        "Agent category does not match the task.",
	"protocol_mismatch":        "Agent protocol version is incompatible.",
	"overview_budget_exceeded": "Agent overview price exceeds the task budget.",
	"formal_budget_exceeded":   "Agent formal price exceeds the task budget.",
	"external_cost_exceeded":   "Agent external cost cap exceeds the task limit.",
	"deadline_unavailable":     "Agent cannot finish before the task deadline.",
	"language_mismatch":        "Agent does not support the task language.",
	"capability_missing":       "Agent is missing a required capability.",
	"risk_not_eligible":        "Agent is not eligible under the current risk policy.",
	"payout_address_invalid":   "Agent payout address is invalid.",
	"vector_version_mismatch":  "Agent vector version is incompatible.",
	"price_invalid":            "Agent price metadata is invalid.",
	"reputation_unavailable":   "Agent reputation is unavailable or invalid.",
}

func HardFilter(request Request, candidates []Candidate) ([]Candidate, []Exclusion) {
	return filterCandidates(request, candidates, true)
}

// CategoryTagsFilter keeps operational, security, category, price and deadline
// gates while omitting the legacy language, capability and vector match gates.
func CategoryTagsFilter(request Request, candidates []Candidate) ([]Candidate, []Exclusion) {
	return filterCandidates(request, candidates, false)
}

func filterCandidates(request Request, candidates []Candidate, legacyMatchGates bool) ([]Candidate, []Exclusion) {
	eligible := make([]Candidate, 0, len(candidates))
	excluded := make([]Exclusion, 0)
	for _, candidate := range candidates {
		reasons := filterReasons(request, candidate, legacyMatchGates)
		if len(reasons) == 0 {
			eligible = append(eligible, candidate)
		} else {
			excluded = append(excluded, Exclusion{AgentID: candidate.AgentID, Candidate: candidate, Reasons: reasons})
		}
	}
	slices.SortFunc(eligible, func(left, right Candidate) int { return strings.Compare(left.AgentID, right.AgentID) })
	slices.SortFunc(excluded, func(left, right Exclusion) int { return strings.Compare(left.AgentID, right.AgentID) })
	return eligible, excluded
}

func filterReasons(request Request, candidate Candidate, legacyMatchGates bool) []Reason {
	codes := make([]string, 0, 8)
	if strings.TrimSpace(candidate.AgentID) == "" || strings.TrimSpace(candidate.ProviderID) == "" {
		codes = append(codes, "agent_identity_invalid")
	}
	if candidate.ProviderID == request.PublisherID {
		codes = append(codes, "self_provider")
	}
	if candidate.Status != "active" {
		codes = append(codes, "status_not_active")
	}
	if candidate.ApprovalStatus != "approved" {
		codes = append(codes, "approval_required")
	}
	if candidate.Health != "healthy" {
		codes = append(codes, "health_not_healthy")
	}
	if candidate.HealthCheckedAt == nil || candidate.HealthValidUntil == nil || candidate.HealthCheckedAt.Before(request.Now.Add(-healthFreshness)) || !candidate.HealthValidUntil.After(request.Now) {
		codes = append(codes, "health_check_stale")
	}
	if candidate.MaxConcurrency < 1 || candidate.ActiveCapacity < 0 || candidate.ActiveCapacity >= candidate.MaxConcurrency {
		codes = append(codes, "capacity_unavailable")
	}
	if !equalFold(candidate.Category, request.Category) {
		codes = append(codes, "category_mismatch")
	}
	if candidate.ProtocolVersion != request.RequiredProtocolVersion {
		codes = append(codes, "protocol_mismatch")
	}
	if legacyMatchGates {
		if !containsFold(candidate.Languages, request.Language) {
			codes = append(codes, "language_mismatch")
		}
		if !containsAllFold(candidate.Capabilities, request.RequiredCapabilities) {
			codes = append(codes, "capability_missing")
		}
	}
	if candidate.RiskStatus != "eligible" {
		codes = append(codes, "risk_not_eligible")
	}
	if !candidate.ReputationAvailable || !validReputation(candidate.Reputation) {
		codes = append(codes, "reputation_unavailable")
	}
	if !walletAddress(candidate.PayoutAddress) {
		codes = append(codes, "payout_address_invalid")
	}
	if legacyMatchGates {
		if candidate.VectorVersion != request.RequiredVectorVersion {
			codes = append(codes, "vector_version_mismatch")
		}
	}
	finishAt := request.Now.Add(candidate.EstimatedDuration)
	if candidate.EstimatedDuration <= 0 || finishAt.After(request.Deadline) {
		codes = append(codes, "deadline_unavailable")
	}
	if invalidMoney(candidate.OverviewPrice) || invalidMoney(candidate.FormalPrice) || invalidMoney(candidate.ExternalCostCap) {
		codes = append(codes, "price_invalid")
	} else {
		if moneyGreater(candidate.OverviewPrice, request.OverviewBudget) {
			codes = append(codes, "overview_budget_exceeded")
		}
		if moneyGreater(candidate.FormalPrice, request.FormalBudget) {
			codes = append(codes, "formal_budget_exceeded")
		}
		if moneyGreater(candidate.ExternalCostCap, request.ExternalCostCap) {
			codes = append(codes, "external_cost_exceeded")
		}
	}
	reasons := make([]Reason, 0, len(codes))
	for _, code := range codes {
		reasons = append(reasons, Reason{Code: code, Message: reasonMessages[code]})
	}
	return reasons
}

func validReputation(value Reputation) bool {
	for _, score := range []int{value.Quality, value.Speed, value.Reliability, value.Communication, value.Compliance} {
		if score < 0 || score > 100 {
			return false
		}
	}
	return true
}

func equalFold(left, right string) bool {
	return strings.EqualFold(strings.TrimSpace(left), strings.TrimSpace(right))
}

func containsFold(values []string, target string) bool {
	return slices.ContainsFunc(values, func(value string) bool { return equalFold(value, target) })
}

func containsAllFold(values, required []string) bool {
	return !slices.ContainsFunc(required, func(item string) bool { return !containsFold(values, item) })
}

func walletAddress(value string) bool {
	if len(value) != 42 || !strings.HasPrefix(value, "0x") {
		return false
	}
	for _, character := range value[2:] {
		if !strings.ContainsRune("0123456789abcdefABCDEF", character) {
			return false
		}
	}
	return true
}

func invalidMoney(value string) bool {
	if value == "" || len(value) > 78 || value != "0" && strings.HasPrefix(value, "0") {
		return true
	}
	for _, character := range value {
		if character < '0' || character > '9' {
			return true
		}
	}
	_, ok := new(big.Int).SetString(value, 10)
	return !ok
}

func moneyGreater(left, right string) bool {
	leftValue, leftOK := new(big.Int).SetString(left, 10)
	rightValue, rightOK := new(big.Int).SetString(right, 10)
	return !leftOK || !rightOK || leftValue.Cmp(rightValue) > 0
}
