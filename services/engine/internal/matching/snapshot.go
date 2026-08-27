package matching

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"time"
)

var ErrSnapshotNotFound = errors.New("matching snapshot not found")

type SnapshotKey struct {
	TaskID             string
	TaskSpecHash       string
	AlgorithmVersion   string
	EffectiveInputHash string
}

type SnapshotDraft struct {
	Key          SnapshotKey
	RuleVersion  string
	ModelVersion string
	Result       Result
}

type Snapshot struct {
	ID                   string
	TaskID               string
	TaskSpecHash         string
	MatchRevision        int
	EffectiveInputHash   string
	AlgorithmVersion     string
	RuleVersion          string
	ModelVersion         string
	SeedDigest           string
	SeedKeyVersion       string
	PolicyHash           string
	ExplorationTriggered bool
	Result               Result
	Selections           []Selection
	CreatedAt            time.Time
}

type SnapshotBuilder func(matchRevision int) (Snapshot, error)

type SnapshotRepository interface {
	Latest(context.Context, string, string, string) (Snapshot, error)
	CreateRevision(context.Context, SnapshotKey, SnapshotBuilder) (Snapshot, bool, error)
}

type SnapshotService struct {
	repository SnapshotRepository
	policy     ShufflePolicy
}

func NewSnapshotService(repository SnapshotRepository, policy ShufflePolicy) (*SnapshotService, error) {
	if repository == nil {
		return nil, errors.New("snapshot repository is required")
	}
	if err := validateShufflePolicy(policy); err != nil {
		return nil, err
	}
	return &SnapshotService{repository: repository, policy: policy}, nil
}

// CreateRevision is called only by an authoritative matching trigger. Browser
// reads and retries must call Latest so that they never reshuffle implicitly.
func (service *SnapshotService) CreateRevision(ctx context.Context, draft SnapshotDraft) (Snapshot, bool, error) {
	if err := validateSnapshotDraft(draft); err != nil {
		return Snapshot{}, false, err
	}
	return service.repository.CreateRevision(ctx, draft.Key, func(revision int) (Snapshot, error) {
		seedContext := SeedContext{
			TaskID:           draft.Key.TaskID,
			TaskSpecHash:     draft.Key.TaskSpecHash,
			MatchRevision:    revision,
			AlgorithmVersion: draft.Key.AlgorithmVersion,
		}
		var shuffle ShuffleResult
		var err error
		var policyHash string
		switch draft.Key.AlgorithmVersion {
		case FairShuffleAlgorithmVersion:
			shuffle, err = FairShuffle(draft.Result.Qualified, seedContext, service.policy)
			policyHash = shufflePolicyHash(draft.Key.AlgorithmVersion, service.policy)
		case CategoryTagsAlgorithmVersion:
			shuffle, err = categoryTagsSelections(draft.Result.Qualified, seedContext)
			policyHash = categoryTagsPolicyHash()
		default:
			err = errors.New("unsupported matching algorithm version")
		}
		if err != nil {
			return Snapshot{}, err
		}
		snapshot := Snapshot{
			ID:                   snapshotID(draft.Key, revision),
			TaskID:               draft.Key.TaskID,
			TaskSpecHash:         draft.Key.TaskSpecHash,
			MatchRevision:        revision,
			EffectiveInputHash:   draft.Key.EffectiveInputHash,
			AlgorithmVersion:     draft.Key.AlgorithmVersion,
			RuleVersion:          draft.RuleVersion,
			ModelVersion:         draft.ModelVersion,
			SeedDigest:           shuffle.SeedDigest,
			SeedKeyVersion:       shuffle.SeedKeyVersion,
			PolicyHash:           policyHash,
			ExplorationTriggered: shuffle.ExplorationTriggered,
			Result:               cloneResult(draft.Result),
			Selections:           slices.Clone(shuffle.Selections),
		}
		return snapshot, validateSnapshot(snapshot)
	})
}

func (service *SnapshotService) Latest(ctx context.Context, taskID, taskSpecHash, algorithmVersion string) (Snapshot, error) {
	if strings.TrimSpace(taskID) == "" || !validSHA256(taskSpecHash) || strings.TrimSpace(algorithmVersion) == "" {
		return Snapshot{}, errors.New("invalid snapshot lookup")
	}
	return service.repository.Latest(ctx, taskID, taskSpecHash, algorithmVersion)
}

func HashEffectiveInput(value any) (string, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("encode effective matching input: %w", err)
	}
	digest := sha256.Sum256(encoded)
	return "sha256:" + hex.EncodeToString(digest[:]), nil
}

func validateSnapshotDraft(draft SnapshotDraft) error {
	if strings.TrimSpace(draft.Key.TaskID) == "" || !validSHA256(draft.Key.TaskSpecHash) || !slices.Contains([]string{FairShuffleAlgorithmVersion, CategoryTagsAlgorithmVersion}, draft.Key.AlgorithmVersion) || !validSHA256(draft.Key.EffectiveInputHash) {
		return errors.New("invalid snapshot identity")
	}
	if strings.TrimSpace(draft.RuleVersion) == "" || strings.TrimSpace(draft.ModelVersion) == "" {
		return errors.New("snapshot rule and model versions are required")
	}
	if len(draft.Result.Qualified) > MaxQualifiedPool || len(draft.Result.Scored) > MaxScoredCandidates {
		return errors.New("snapshot candidate limits exceeded")
	}
	scored := make(map[string]ScoredCandidate, len(draft.Result.Scored))
	for _, candidate := range draft.Result.Scored {
		if _, duplicate := scored[candidate.Candidate.AgentID]; duplicate {
			return fmt.Errorf("duplicate scored agent %q", candidate.Candidate.AgentID)
		}
		scored[candidate.Candidate.AgentID] = candidate
	}
	for _, candidate := range draft.Result.Qualified {
		scoredCandidate, ok := scored[candidate.Candidate.AgentID]
		if !ok {
			return fmt.Errorf("qualified agent %q is not in scored candidates", candidate.Candidate.AgentID)
		}
		if !reflect.DeepEqual(scoredCandidate, candidate) {
			return fmt.Errorf("qualified agent %q differs from its scored record", candidate.Candidate.AgentID)
		}
	}
	expectedScored := slices.Clone(draft.Result.Scored)
	expectedQualified := []ScoredCandidate(nil)
	if draft.Key.AlgorithmVersion == CategoryTagsAlgorithmVersion {
		if draft.Result.Strategy != CategoryTagsStrategy {
			return errors.New("category-tags snapshot requires category-tags result")
		}
		sortCategoryTags(expectedScored)
		expectedQualified = qualifyCategoryTags(expectedScored)
	} else {
		if draft.Result.Strategy != "" {
			return errors.New("fair-shuffle snapshot requires legacy matching result")
		}
		sortScored(expectedScored)
		expectedQualified = qualify(expectedScored)
	}
	if !reflect.DeepEqual(expectedScored, draft.Result.Scored) || !reflect.DeepEqual(expectedQualified, draft.Result.Qualified) {
		return errors.New("snapshot scored order or qualified pool is not canonical")
	}
	excluded := make(map[string]struct{}, len(draft.Result.Excluded))
	for _, exclusion := range draft.Result.Excluded {
		if exclusion.AgentID != exclusion.Candidate.AgentID {
			return fmt.Errorf("excluded agent %q identity does not match candidate", exclusion.AgentID)
		}
		if _, duplicate := excluded[exclusion.AgentID]; duplicate {
			return fmt.Errorf("duplicate excluded agent %q", exclusion.AgentID)
		}
		excluded[exclusion.AgentID] = struct{}{}
		if _, overlap := scored[exclusion.AgentID]; overlap {
			return fmt.Errorf("agent %q is both scored and excluded", exclusion.AgentID)
		}
		if len(exclusion.Reasons) == 0 {
			return fmt.Errorf("excluded agent %q has no reasons", exclusion.AgentID)
		}
	}
	return nil
}

func validateSnapshot(snapshot Snapshot) error {
	if !validSHA256(snapshot.ID) || snapshot.MatchRevision < 1 || !validSHA256(snapshot.SeedDigest) || !validSHA256(snapshot.PolicyHash) {
		return errors.New("incomplete immutable snapshot")
	}
	qualified := make(map[string]struct{}, len(snapshot.Result.Qualified))
	for _, candidate := range snapshot.Result.Qualified {
		qualified[candidate.Candidate.AgentID] = struct{}{}
	}
	seenPositions := make(map[int]struct{}, len(snapshot.Selections))
	for _, selection := range snapshot.Selections {
		if _, ok := qualified[selection.Candidate.Candidate.AgentID]; !ok {
			return errors.New("snapshot selection is outside qualified pool")
		}
		if selection.Position < 1 || selection.Position > DefaultSelectionLimit {
			return errors.New("snapshot selection position is invalid")
		}
		if _, duplicate := seenPositions[selection.Position]; duplicate {
			return errors.New("snapshot selection positions are not unique")
		}
		seenPositions[selection.Position] = struct{}{}
		if selection.Exploration && selection.Position != 3 {
			return errors.New("snapshot exploration escaped third position")
		}
	}
	return nil
}

func validateShufflePolicy(policy ShufflePolicy) error {
	return validateShuffleInput(nil, SeedContext{
		TaskID:           "policy-validation",
		TaskSpecHash:     "sha256:0000000000000000000000000000000000000000000000000000000000000000",
		MatchRevision:    1,
		AlgorithmVersion: FairShuffleAlgorithmVersion,
	}, policy)
}

func snapshotID(key SnapshotKey, revision int) string {
	digest := sha256.New()
	writeSeedField(digest, key.TaskID)
	writeSeedField(digest, key.TaskSpecHash)
	writeSeedField(digest, key.AlgorithmVersion)
	writeSeedField(digest, fmt.Sprintf("%d", revision))
	return "sha256:" + hex.EncodeToString(digest.Sum(nil))
}

func shufflePolicyHash(algorithmVersion string, policy ShufflePolicy) string {
	encoded := fmt.Sprintf("%s\x00%s\x00%d\x00%d\x00%d\x00%d\x00%d", algorithmVersion, policy.SeedKeyVersion, policy.ExplorationBasisPoints, policy.ProviderCap, policy.SelectionLimit, ExplorationExposureLimit, ExplorationSampleLimit)
	digest := sha256.Sum256([]byte(encoded))
	return "sha256:" + hex.EncodeToString(digest[:])
}

func cloneResult(source Result) Result {
	encoded, err := json.Marshal(source)
	if err != nil {
		panic(fmt.Sprintf("clone matching result: %v", err))
	}
	var result Result
	if err = json.Unmarshal(encoded, &result); err != nil {
		panic(fmt.Sprintf("clone matching result: %v", err))
	}
	return result
}
