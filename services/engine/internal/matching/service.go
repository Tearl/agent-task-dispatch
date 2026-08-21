package matching

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
)

const reciprocalRankConstant = 60

type Service struct {
	adapters []RecallAdapter
	adjuster RankingAdjuster
}

func NewService(dense RecallAdapter, adjuster RankingAdjuster) *Service {
	adapters := make([]RecallAdapter, 0, 3)
	if dense != nil {
		adapters = append(adapters, dense)
	}
	adapters = append(adapters, LexicalRecall{}, ExactRecall{})
	return &Service{adapters: adapters, adjuster: adjuster}
}

// NewServiceWithAdapters is intended for composition roots and deterministic
// dependency-failure tests. Production callers should normally use NewService.
func NewServiceWithAdapters(adapters []RecallAdapter, adjuster RankingAdjuster) *Service {
	return &Service{adapters: slices.Clone(adapters), adjuster: adjuster}
}

func (service *Service) Match(ctx context.Context, request Request, candidates []Candidate) (Result, error) {
	if err := validateRequest(request); err != nil {
		return Result{}, err
	}

	eligible, excluded := HardFilter(request, candidates)
	result := Result{Excluded: excluded}
	byID := make(map[string]Candidate, len(eligible))
	for _, candidate := range eligible {
		if _, exists := byID[candidate.AgentID]; exists {
			return Result{}, fmt.Errorf("duplicate eligible agent id %q", candidate.AgentID)
		}
		byID[candidate.AgentID] = candidate
	}

	evidence, degradations := service.recall(ctx, request, eligible, byID)
	result.Degradations = append(result.Degradations, degradations...)
	for _, agentID := range fusedAgentIDs(evidence) {
		candidate := byID[agentID]
		score := RuleScore(request, candidate, evidence[agentID])
		delta := 0
		if service.adjuster != nil {
			adjusted, err := service.adjuster.Adjust(ctx, request, candidate, score)
			if err != nil {
				result.Degradations = appendUniqueDegradation(result.Degradations, Degradation{
					Dependency: "ranking_model",
					Code:       "model_unavailable",
					Message:    "Ranking model unavailable; fixed ModelDelta=0 fallback applied.",
				})
			} else {
				delta = adjusted
			}
		} else {
			result.Degradations = appendUniqueDegradation(result.Degradations, Degradation{
				Dependency: "ranking_model",
				Code:       "model_disabled",
				Message:    "Ranking model disabled; fixed ModelDelta=0 fallback applied.",
			})
		}
		result.Scored = append(result.Scored, ScoredCandidate{
			Candidate: candidate,
			Recall:    cloneEvidence(evidence[agentID]),
			Score:     ApplyModelDelta(score, delta),
		})
	}

	sortScored(result.Scored)
	result.Qualified = qualify(result.Scored)
	sortDegradations(result.Degradations)
	return result, nil
}

func (service *Service) recall(ctx context.Context, request Request, eligible []Candidate, byID map[string]Candidate) (map[string]map[string]RecallEvidence, []Degradation) {
	evidence := make(map[string]map[string]RecallEvidence)
	degradations := make([]Degradation, 0)
	seenChannels := make(map[string]struct{}, 3)

	for _, adapter := range service.adapters {
		if adapter == nil {
			continue
		}
		channel := adapter.Channel()
		if channel != ChannelDense && channel != ChannelLexical && channel != ChannelExact {
			degradations = appendUniqueDegradation(degradations, Degradation{Dependency: channel, Code: "unknown_recall_channel", Message: "Unknown recall channel ignored."})
			continue
		}
		if _, duplicate := seenChannels[channel]; duplicate {
			degradations = appendUniqueDegradation(degradations, Degradation{Dependency: channel, Code: "duplicate_recall_channel", Message: "Duplicate recall channel ignored."})
			continue
		}
		seenChannels[channel] = struct{}{}

		hits, err := adapter.Recall(ctx, request, slices.Clone(eligible), MaxRecallPerChannel)
		if err != nil {
			degradations = appendUniqueDegradation(degradations, Degradation{Dependency: channel, Code: "recall_unavailable", Message: "Recall channel unavailable; remaining channels used."})
			continue
		}
		normalized, invalid := normalizeHits(hits, byID)
		if invalid {
			degradations = appendUniqueDegradation(degradations, Degradation{Dependency: channel, Code: "invalid_recall_hit", Message: "Invalid or ineligible recall hits ignored."})
		}
		for index, hit := range normalized[:min(len(normalized), MaxRecallPerChannel)] {
			if evidence[hit.AgentID] == nil {
				evidence[hit.AgentID] = make(map[string]RecallEvidence, 3)
			}
			evidence[hit.AgentID][channel] = RecallEvidence{Rank: index + 1, Relevance: hit.Relevance}
		}
	}

	for _, channel := range []string{ChannelDense, ChannelLexical, ChannelExact} {
		if _, ok := seenChannels[channel]; !ok {
			degradations = appendUniqueDegradation(degradations, Degradation{Dependency: channel, Code: "recall_not_configured", Message: "Recall channel not configured; remaining channels used."})
		}
	}
	return evidence, degradations
}

func validateRequest(request Request) error {
	if strings.TrimSpace(request.TaskID) == "" || strings.TrimSpace(request.PublisherID) == "" || strings.TrimSpace(request.Category) == "" || strings.TrimSpace(request.Language) == "" || strings.TrimSpace(request.RequiredProtocolVersion) == "" || strings.TrimSpace(request.RequiredVectorVersion) == "" {
		return errors.New("matching request requires task, publisher, category, language, protocol, and vector versions")
	}
	if request.Now.IsZero() || !request.Deadline.After(request.Now) {
		return errors.New("matching request requires a future deadline and explicit current time")
	}
	for field, value := range map[string]string{
		"overview budget": request.OverviewBudget,
		"formal budget":   request.FormalBudget,
		"external cap":    request.ExternalCostCap,
	} {
		if invalidMoney(value) {
			return fmt.Errorf("matching request has invalid %s", field)
		}
	}
	return nil
}

func normalizeHits(hits []RecallHit, eligible map[string]Candidate) ([]RecallHit, bool) {
	best := make(map[string]int, len(hits))
	invalid := false
	for _, hit := range hits {
		if _, ok := eligible[hit.AgentID]; !ok || hit.Relevance < 0 || hit.Relevance > 10_000 {
			invalid = true
			continue
		}
		if relevance, exists := best[hit.AgentID]; !exists || hit.Relevance > relevance {
			best[hit.AgentID] = hit.Relevance
		}
	}
	normalized := make([]RecallHit, 0, len(best))
	for agentID, relevance := range best {
		normalized = append(normalized, RecallHit{AgentID: agentID, Relevance: relevance})
	}
	sortHits(normalized)
	return normalized, invalid
}

func fusedAgentIDs(evidence map[string]map[string]RecallEvidence) []string {
	ids := make([]string, 0, len(evidence))
	for agentID := range evidence {
		ids = append(ids, agentID)
	}
	slices.SortFunc(ids, func(left, right string) int {
		leftScore := reciprocalRankScore(evidence[left])
		rightScore := reciprocalRankScore(evidence[right])
		if leftScore != rightScore {
			if leftScore > rightScore {
				return -1
			}
			return 1
		}
		return strings.Compare(left, right)
	})
	return ids[:min(len(ids), MaxScoredCandidates)]
}

func reciprocalRankScore(evidence map[string]RecallEvidence) int {
	total := 0
	for _, item := range evidence {
		if item.Rank > 0 {
			total += 1_000_000 / (reciprocalRankConstant + item.Rank)
		}
	}
	return total
}

func qualify(scored []ScoredCandidate) []ScoredCandidate {
	if len(scored) == 0 {
		return nil
	}
	best := scored[0].Score.RankingScore
	qualified := make([]ScoredCandidate, 0, min(len(scored), MaxQualifiedPool))
	for _, candidate := range scored {
		if candidate.Score.RuleScore < QualificationFloor || candidate.Score.RankingScore < QualificationFloor || candidate.Score.RankingScore < best-MaximumScoreGap {
			continue
		}
		qualified = append(qualified, candidate)
		if len(qualified) == MaxQualifiedPool {
			break
		}
	}
	return qualified
}

func sortScored(candidates []ScoredCandidate) {
	slices.SortFunc(candidates, func(left, right ScoredCandidate) int {
		if left.Score.RankingScore != right.Score.RankingScore {
			return right.Score.RankingScore - left.Score.RankingScore
		}
		if left.Score.RuleScore != right.Score.RuleScore {
			return right.Score.RuleScore - left.Score.RuleScore
		}
		return strings.Compare(left.Candidate.AgentID, right.Candidate.AgentID)
	})
}

func cloneEvidence(source map[string]RecallEvidence) map[string]RecallEvidence {
	result := make(map[string]RecallEvidence, len(source))
	for channel, evidence := range source {
		result[channel] = evidence
	}
	return result
}

func appendUniqueDegradation(existing []Degradation, item Degradation) []Degradation {
	for _, candidate := range existing {
		if candidate.Dependency == item.Dependency && candidate.Code == item.Code {
			return existing
		}
	}
	return append(existing, item)
}

func sortDegradations(degradations []Degradation) {
	slices.SortFunc(degradations, func(left, right Degradation) int {
		if comparison := strings.Compare(left.Dependency, right.Dependency); comparison != 0 {
			return comparison
		}
		return strings.Compare(left.Code, right.Code)
	})
}
