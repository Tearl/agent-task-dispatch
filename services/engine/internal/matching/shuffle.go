package matching

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"strings"
)

const (
	FairShuffleAlgorithmVersion = "fair-shuffle-v1"
	DefaultExplorationBPS       = 1_500
	DefaultProviderCap          = 1
	DefaultSelectionLimit       = 3
	ExplorationExposureLimit    = 100
	ExplorationSampleLimit      = 20
)

type SeedContext struct {
	TaskID           string
	TaskSpecHash     string
	MatchRevision    int
	AlgorithmVersion string
}

type ShufflePolicy struct {
	SeedKeyVersion         string
	SeedSecret             []byte
	ExplorationBasisPoints int
	ProviderCap            int
	SelectionLimit         int
}

type Selection struct {
	Candidate              ScoredCandidate
	Position               int
	Weight                 int
	ProbabilityNumerator   int
	ProbabilityDenominator int
	RandomDraw             uint64
	Exploration            bool
}

type ShuffleResult struct {
	AlgorithmVersion     string
	SeedDigest           string
	SeedKeyVersion       string
	ExplorationTriggered bool
	Selections           []Selection
}

func DefaultShufflePolicy(seedKeyVersion string, seedSecret []byte) ShufflePolicy {
	return ShufflePolicy{
		SeedKeyVersion:         seedKeyVersion,
		SeedSecret:             slices.Clone(seedSecret),
		ExplorationBasisPoints: DefaultExplorationBPS,
		ProviderCap:            DefaultProviderCap,
		SelectionLimit:         DefaultSelectionLimit,
	}
}

func FairShuffle(pool []ScoredCandidate, seedContext SeedContext, policy ShufflePolicy) (ShuffleResult, error) {
	if err := validateShuffleInput(pool, seedContext, policy); err != nil {
		return ShuffleResult{}, err
	}
	seed := deriveSeed(seedContext, policy)
	digest := sha256.Sum256(seed)
	result := ShuffleResult{
		AlgorithmVersion:     seedContext.AlgorithmVersion,
		SeedDigest:           "sha256:" + hex.EncodeToString(digest[:]),
		SeedKeyVersion:       policy.SeedKeyVersion,
		ExplorationTriggered: deterministicDraw(seed, "exploration", 10_000) < uint64(policy.ExplorationBasisPoints),
	}

	remaining := slices.Clone(pool)
	providerSelections := make(map[string]int)
	for position := 1; position <= policy.SelectionLimit; position++ {
		available := providerEligible(remaining, providerSelections, policy.ProviderCap)
		if len(available) == 0 {
			break
		}
		exploration := false
		drawPool := available
		if position == 3 && result.ExplorationTriggered {
			explorationPool := slices.DeleteFunc(slices.Clone(available), func(candidate ScoredCandidate) bool {
				return !isExplorationCandidate(candidate.Candidate)
			})
			if len(explorationPool) > 0 {
				drawPool = explorationPool
				exploration = true
			}
		}
		selected, draw, totalWeight := weightedDraw(seed, fmt.Sprintf("position:%d:exploration:%t", position, exploration), drawPool)
		weight := selectionWeight(selected.Score.RankingScore)
		result.Selections = append(result.Selections, Selection{
			Candidate:              selected,
			Position:               position,
			Weight:                 weight,
			ProbabilityNumerator:   weight,
			ProbabilityDenominator: totalWeight,
			RandomDraw:             draw,
			Exploration:            exploration,
		})
		providerSelections[selected.Candidate.ProviderID]++
		remaining = slices.DeleteFunc(remaining, func(candidate ScoredCandidate) bool {
			return candidate.Candidate.AgentID == selected.Candidate.AgentID
		})
	}
	return result, nil
}

func validateShuffleInput(pool []ScoredCandidate, seedContext SeedContext, policy ShufflePolicy) error {
	if strings.TrimSpace(seedContext.TaskID) == "" || !validSHA256(seedContext.TaskSpecHash) || seedContext.MatchRevision < 1 || seedContext.AlgorithmVersion != FairShuffleAlgorithmVersion {
		return errors.New("invalid fair shuffle seed context")
	}
	if strings.TrimSpace(policy.SeedKeyVersion) == "" || len(policy.SeedSecret) < 32 {
		return errors.New("fair shuffle requires a versioned secret of at least 32 bytes")
	}
	if policy.ExplorationBasisPoints < 0 || policy.ExplorationBasisPoints > 10_000 || policy.ProviderCap < 1 || policy.SelectionLimit < 1 || policy.SelectionLimit > DefaultSelectionLimit {
		return errors.New("invalid fair shuffle policy")
	}
	if len(pool) > MaxQualifiedPool {
		return fmt.Errorf("qualified pool exceeds %d candidates", MaxQualifiedPool)
	}
	best := 0
	for _, candidate := range pool {
		if candidate.Score.RankingScore > best {
			best = candidate.Score.RankingScore
		}
	}
	seenAgents := make(map[string]struct{}, len(pool))
	for _, candidate := range pool {
		if strings.TrimSpace(candidate.Candidate.AgentID) == "" || strings.TrimSpace(candidate.Candidate.ProviderID) == "" {
			return errors.New("fair shuffle candidate identity is required")
		}
		if _, duplicate := seenAgents[candidate.Candidate.AgentID]; duplicate {
			return fmt.Errorf("duplicate fair shuffle agent %q", candidate.Candidate.AgentID)
		}
		seenAgents[candidate.Candidate.AgentID] = struct{}{}
		if candidate.Score.RuleScore < QualificationFloor || candidate.Score.RankingScore < QualificationFloor || candidate.Score.RankingScore < best-MaximumScoreGap {
			return fmt.Errorf("agent %q is outside the qualified pool", candidate.Candidate.AgentID)
		}
		if candidate.Candidate.ExposureCount < 0 || candidate.Candidate.EffectiveSamples < 0 {
			return fmt.Errorf("agent %q has invalid exploration counters", candidate.Candidate.AgentID)
		}
	}
	return nil
}

func deriveSeed(seedContext SeedContext, policy ShufflePolicy) []byte {
	mac := hmac.New(sha256.New, policy.SeedSecret)
	writeSeedField(mac, seedContext.TaskID)
	writeSeedField(mac, seedContext.TaskSpecHash)
	var revision [8]byte
	binary.BigEndian.PutUint64(revision[:], uint64(seedContext.MatchRevision))
	_, _ = mac.Write(revision[:])
	writeSeedField(mac, seedContext.AlgorithmVersion)
	writeSeedField(mac, policy.SeedKeyVersion)
	return mac.Sum(nil)
}

type hashWriter interface {
	Write([]byte) (int, error)
}

func writeSeedField(writer hashWriter, value string) {
	var length [4]byte
	binary.BigEndian.PutUint32(length[:], uint32(len(value)))
	_, _ = writer.Write(length[:])
	_, _ = writer.Write([]byte(value))
}

func deterministicDraw(seed []byte, domain string, bound uint64) uint64 {
	if bound == 0 {
		panic("deterministic draw bound must be positive")
	}
	threshold := -bound % bound
	for counter := uint64(0); ; counter++ {
		mac := hmac.New(sha256.New, seed)
		writeSeedField(mac, domain)
		var encodedCounter [8]byte
		binary.BigEndian.PutUint64(encodedCounter[:], counter)
		_, _ = mac.Write(encodedCounter[:])
		value := binary.BigEndian.Uint64(mac.Sum(nil)[:8])
		if value >= threshold {
			return value % bound
		}
	}
}

func weightedDraw(seed []byte, domain string, pool []ScoredCandidate) (ScoredCandidate, uint64, int) {
	stable := slices.Clone(pool)
	slices.SortFunc(stable, func(left, right ScoredCandidate) int {
		return strings.Compare(left.Candidate.AgentID, right.Candidate.AgentID)
	})
	total := 0
	for _, candidate := range stable {
		total += selectionWeight(candidate.Score.RankingScore)
	}
	draw := deterministicDraw(seed, domain, uint64(total))
	cursor := uint64(0)
	for _, candidate := range stable {
		cursor += uint64(selectionWeight(candidate.Score.RankingScore))
		if draw < cursor {
			return candidate, draw, total
		}
	}
	panic("weighted draw did not select a candidate")
}

func providerEligible(pool []ScoredCandidate, selected map[string]int, cap int) []ScoredCandidate {
	return slices.DeleteFunc(slices.Clone(pool), func(candidate ScoredCandidate) bool {
		return selected[candidate.Candidate.ProviderID] >= cap
	})
}

func selectionWeight(rankingScore int) int {
	return max(1, rankingScore-QualificationFloor+1)
}

func isExplorationCandidate(candidate Candidate) bool {
	return candidate.ExposureCount < ExplorationExposureLimit || candidate.EffectiveSamples < ExplorationSampleLimit
}

func validSHA256(value string) bool {
	if len(value) != len("sha256:")+sha256.Size*2 || !strings.HasPrefix(value, "sha256:") {
		return false
	}
	_, err := hex.DecodeString(strings.TrimPrefix(value, "sha256:"))
	return err == nil
}
