package matching

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"strings"
)

const (
	CategoryTagsAlgorithmVersion = "category-tags-v1"
	CategoryTagsStrategy         = "category-tags"
)

// MatchCategoryTags is the deliberately simple current matching strategy.
// Category is a hard gate and normalized tag intersection is the only ranking
// signal. The legacy recall, score and fair-shuffle pipeline remains available
// through Match and FairShuffle.
func (service *Service) MatchCategoryTags(ctx context.Context, request Request, candidates []Candidate) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	if err := validateCategoryTagsRequest(request); err != nil {
		return Result{}, err
	}

	eligible, excluded := CategoryTagsFilter(request, candidates)
	result := Result{
		Strategy:     CategoryTagsStrategy,
		Qualified:    make([]ScoredCandidate, 0),
		Scored:       make([]ScoredCandidate, 0, len(eligible)),
		Excluded:     excluded,
		Degradations: make([]Degradation, 0),
	}
	seen := make(map[string]struct{}, len(eligible))
	requestTags := normalizedSet(request.Tags)
	for _, candidate := range eligible {
		if _, duplicate := seen[candidate.AgentID]; duplicate {
			return Result{}, fmt.Errorf("duplicate eligible agent id %q", candidate.AgentID)
		}
		seen[candidate.AgentID] = struct{}{}
		overlap := setOverlap(requestTags, normalizedSet(candidate.Tags))
		tagScore := tagMatchScore(overlap, len(requestTags))
		result.Scored = append(result.Scored, ScoredCandidate{
			Candidate: candidate,
			Recall:    make(map[string]RecallEvidence),
			Score: ScoreBreakdown{
				TagOverlap:   overlap,
				TaskMatch:    tagScore,
				RuleScore:    QualificationFloor + tagScore,
				RankingScore: QualificationFloor + tagScore,
			},
		})
	}

	sortCategoryTags(result.Scored)
	if len(result.Scored) > MaxScoredCandidates {
		result.Scored = result.Scored[:MaxScoredCandidates]
	}
	result.Qualified = qualifyCategoryTags(result.Scored)
	return result, nil
}

func validateCategoryTagsRequest(request Request) error {
	if strings.TrimSpace(request.TaskID) == "" || strings.TrimSpace(request.PublisherID) == "" || strings.TrimSpace(request.Category) == "" || strings.TrimSpace(request.RequiredProtocolVersion) == "" {
		return errors.New("category-tags matching request requires task, publisher, category, and protocol")
	}
	if request.Now.IsZero() || !request.Deadline.After(request.Now) {
		return errors.New("category-tags matching request requires a future deadline and explicit current time")
	}
	for field, value := range map[string]string{
		"overview budget": request.OverviewBudget,
		"formal budget":   request.FormalBudget,
		"external cap":    request.ExternalCostCap,
	} {
		if invalidMoney(value) {
			return fmt.Errorf("category-tags matching request has invalid %s", field)
		}
	}
	return nil
}

func normalizedSet(values []string) map[string]struct{} {
	result := make(map[string]struct{}, len(values))
	for _, value := range values {
		if normalized := strings.ToLower(strings.TrimSpace(value)); normalized != "" {
			result[normalized] = struct{}{}
		}
	}
	return result
}

func setOverlap(left, right map[string]struct{}) int {
	count := 0
	for value := range left {
		if _, ok := right[value]; ok {
			count++
		}
	}
	return count
}

func tagMatchScore(overlap, taskTagCount int) int {
	if taskTagCount == 0 {
		return 0
	}
	return overlap * 40 / taskTagCount
}

func sortCategoryTags(candidates []ScoredCandidate) {
	slices.SortFunc(candidates, func(left, right ScoredCandidate) int {
		if left.Score.TagOverlap != right.Score.TagOverlap {
			return right.Score.TagOverlap - left.Score.TagOverlap
		}
		return strings.Compare(left.Candidate.AgentID, right.Candidate.AgentID)
	})
}

func qualifyCategoryTags(scored []ScoredCandidate) []ScoredCandidate {
	return slices.Clone(scored[:min(len(scored), MaxQualifiedPool)])
}

func categoryTagsSelections(pool []ScoredCandidate, seedContext SeedContext) (ShuffleResult, error) {
	if strings.TrimSpace(seedContext.TaskID) == "" || !validSHA256(seedContext.TaskSpecHash) || seedContext.MatchRevision < 1 || seedContext.AlgorithmVersion != CategoryTagsAlgorithmVersion {
		return ShuffleResult{}, errors.New("invalid category-tags selection context")
	}
	if len(pool) > MaxQualifiedPool {
		return ShuffleResult{}, fmt.Errorf("qualified pool exceeds %d candidates", MaxQualifiedPool)
	}
	stable := slices.Clone(pool)
	sortCategoryTags(stable)
	seen := make(map[string]struct{}, len(stable))
	digest := sha256.New()
	writeSeedField(digest, seedContext.TaskID)
	writeSeedField(digest, seedContext.TaskSpecHash)
	writeSeedField(digest, seedContext.AlgorithmVersion)
	for _, candidate := range stable {
		if strings.TrimSpace(candidate.Candidate.AgentID) == "" || strings.TrimSpace(candidate.Candidate.ProviderID) == "" {
			return ShuffleResult{}, errors.New("category-tags candidate identity is required")
		}
		if _, duplicate := seen[candidate.Candidate.AgentID]; duplicate {
			return ShuffleResult{}, fmt.Errorf("duplicate category-tags agent %q", candidate.Candidate.AgentID)
		}
		seen[candidate.Candidate.AgentID] = struct{}{}
		writeSeedField(digest, candidate.Candidate.AgentID)
	}

	result := ShuffleResult{
		AlgorithmVersion: seedContext.AlgorithmVersion,
		SeedDigest:       "sha256:" + hex.EncodeToString(digest.Sum(nil)),
		SeedKeyVersion:   "not-used",
	}
	for index, candidate := range stable[:min(len(stable), DefaultSelectionLimit)] {
		result.Selections = append(result.Selections, Selection{
			Candidate:              candidate,
			Position:               index + 1,
			Weight:                 candidate.Score.TagOverlap + 1,
			ProbabilityNumerator:   1,
			ProbabilityDenominator: 1,
		})
	}
	return result, nil
}

func categoryTagsPolicyHash() string {
	digest := sha256.Sum256([]byte(CategoryTagsAlgorithmVersion + "\x00category-exact\x00tag-overlap-desc\x00agent-id-asc\x00selection-limit:3"))
	return "sha256:" + hex.EncodeToString(digest[:])
}
