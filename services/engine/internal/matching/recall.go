package matching

import (
	"context"
	"slices"
	"strings"
	"unicode"
)

type ExactRecall struct{}

func (ExactRecall) Channel() string { return ChannelExact }
func (ExactRecall) Recall(_ context.Context, _ Request, candidates []Candidate, limit int) ([]RecallHit, error) {
	hits := make([]RecallHit, 0, min(limit, len(candidates)))
	for _, candidate := range candidates {
		if len(hits) == limit {
			break
		}
		hits = append(hits, RecallHit{AgentID: candidate.AgentID, Relevance: 10_000})
	}
	return hits, nil
}

type LexicalRecall struct{}

func (LexicalRecall) Channel() string { return ChannelLexical }
func (LexicalRecall) Recall(_ context.Context, request Request, candidates []Candidate, limit int) ([]RecallHit, error) {
	queryTerms := append([]string{}, request.Terms...)
	queryTerms = append(queryTerms, request.RequiredCapabilities...)
	queryTerms = append(queryTerms, request.Category)
	query := tokenSet(queryTerms)
	hits := make([]RecallHit, 0, len(candidates))
	for _, candidate := range candidates {
		terms := append(append(append([]string{}, candidate.Tags...), candidate.Capabilities...), candidate.Category)
		relevance := overlapBasisPoints(query, tokenSet(terms))
		if relevance > 0 {
			hits = append(hits, RecallHit{AgentID: candidate.AgentID, Relevance: relevance})
		}
	}
	sortHits(hits)
	return hits[:min(limit, len(hits))], nil
}

func sortHits(hits []RecallHit) {
	slices.SortFunc(hits, func(left, right RecallHit) int {
		if left.Relevance != right.Relevance {
			return right.Relevance - left.Relevance
		}
		return strings.Compare(left.AgentID, right.AgentID)
	})
}

func tokenSet(values []string) map[string]struct{} {
	result := make(map[string]struct{})
	for _, value := range values {
		for _, token := range strings.FieldsFunc(strings.ToLower(value), func(character rune) bool {
			return unicode.IsSpace(character) || unicode.IsPunct(character)
		}) {
			if token != "" {
				result[token] = struct{}{}
			}
		}
	}
	return result
}

func overlapBasisPoints(left, right map[string]struct{}) int {
	if len(left) == 0 || len(right) == 0 {
		return 0
	}
	intersection := 0
	for token := range left {
		if _, ok := right[token]; ok {
			intersection++
		}
	}
	return intersection * 10_000 / len(left)
}
