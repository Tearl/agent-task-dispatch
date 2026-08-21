package matching

import (
	"math/big"
	"time"
)

const (
	denseTaskWeight   = 25
	lexicalTaskWeight = 15
	exactTaskWeight   = 20
)

func RuleScore(request Request, candidate Candidate, recall map[string]RecallEvidence) ScoreBreakdown {
	taskMatch := weightedRelevance(recall[ChannelDense].Relevance, denseTaskWeight) +
		weightedRelevance(recall[ChannelLexical].Relevance, lexicalTaskWeight) +
		weightedRelevance(recall[ChannelExact].Relevance, exactTaskWeight)
	reputation := reputationScore(candidate.Reputation)
	priceTime := priceHeadroomScore(candidate.FormalPrice, request.FormalBudget) +
		timeHeadroomScore(candidate.EstimatedDuration, request.Deadline.Sub(request.Now))
	availability := ratioScore(candidate.MaxConcurrency-candidate.ActiveCapacity, candidate.MaxConcurrency, 5)

	return ScoreBreakdown{
		TaskMatch:    taskMatch,
		Reputation:   reputation,
		PriceTime:    priceTime,
		Availability: availability,
		RuleScore:    taskMatch + reputation + priceTime + availability,
	}
}

func ApplyModelDelta(score ScoreBreakdown, delta int) ScoreBreakdown {
	score.ModelDelta = clamp(delta, -5, 5)
	score.RankingScore = clamp(score.RuleScore+score.ModelDelta, 0, 100)
	return score
}

func weightedRelevance(relevance, maximum int) int {
	return clamp(relevance, 0, 10_000) * maximum / 10_000
}

func reputationScore(reputation Reputation) int {
	values := []int{
		reputation.Quality,
		reputation.Speed,
		reputation.Reliability,
		reputation.Communication,
		reputation.Compliance,
	}
	total := 0
	for _, value := range values {
		total += clamp(value, 0, 100) * 5 / 100
	}
	return total
}

func priceHeadroomScore(price, budget string) int {
	priceValue, priceOK := new(big.Int).SetString(price, 10)
	budgetValue, budgetOK := new(big.Int).SetString(budget, 10)
	if !priceOK || !budgetOK || priceValue.Sign() < 0 || budgetValue.Sign() < 0 || priceValue.Cmp(budgetValue) > 0 {
		return 0
	}
	if budgetValue.Sign() == 0 {
		return 5
	}
	remaining := new(big.Int).Sub(budgetValue, priceValue)
	points := new(big.Int).Mul(remaining, big.NewInt(5))
	points.Div(points, budgetValue)
	return int(points.Int64())
}

func timeHeadroomScore(duration, available time.Duration) int {
	if duration <= 0 || available <= 0 || duration > available {
		return 0
	}
	return ratioScore(int(available-duration), int(available), 5)
}

func ratioScore(numerator, denominator, maximum int) int {
	if denominator <= 0 || numerator <= 0 {
		return 0
	}
	bounded := clamp(numerator, 0, denominator)
	return (bounded*maximum + denominator/2) / denominator
}

func clamp(value, minimum, maximum int) int {
	if value < minimum {
		return minimum
	}
	if value > maximum {
		return maximum
	}
	return value
}
