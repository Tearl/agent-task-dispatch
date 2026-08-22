package chain

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/example/agent-platform/engine/internal/selection"
)

type Projector struct {
	source     Source
	repository Repository
	scope      Scope
	now        func() time.Time
}

func NewProjector(source Source, repository Repository, scope Scope) (*Projector, error) {
	if source == nil || repository == nil || !validScope(scope) {
		return nil, ErrInvalidInput
	}
	return &Projector{source: source, repository: repository, scope: normalizeScope(scope), now: func() time.Time { return time.Now().UTC() }}, nil
}

func (projector *Projector) SyncOnce(ctx context.Context) (Cursor, error) {
	chainID, err := projector.source.ChainID(ctx)
	if err != nil {
		return Cursor{}, err
	}
	if chainID != projector.scope.ChainID {
		return Cursor{}, fmt.Errorf("%w: RPC chain id mismatch", ErrInvalidInput)
	}
	head, err := projector.source.Head(ctx)
	if err != nil {
		return Cursor{}, err
	}
	if head+1 < projector.scope.Confirmations {
		return projector.repository.Cursor(ctx, projector.scope)
	}
	safeHead := head - (projector.scope.Confirmations - 1)
	for {
		cursor, cursorErr := projector.repository.Cursor(ctx, projector.scope)
		if cursorErr != nil {
			return Cursor{}, cursorErr
		}
		next := projector.scope.StartBlock
		if cursor.Set {
			if cursor.Height >= safeHead {
				return cursor, nil
			}
			next = cursor.Height + 1
		}
		if next > safeHead {
			return cursor, nil
		}
		block, blockErr := projector.source.Block(ctx, next)
		if blockErr != nil {
			return cursor, blockErr
		}
		if block.Number != next || !validHash(block.Hash) || (next > 0 && !validHash(block.ParentHash)) {
			return cursor, ErrGap
		}
		if cursor.Set && block.ParentHash != cursor.Hash {
			ancestor, findErr := projector.commonAncestor(ctx, cursor)
			if findErr != nil {
				return cursor, findErr
			}
			if rewindErr := projector.repository.Rewind(ctx, projector.scope, ancestor, "chain_reorganization"); rewindErr != nil {
				return cursor, rewindErr
			}
			continue
		}
		events, decodeErr := DecodeBlock(projector.scope, block)
		if decodeErr != nil {
			return cursor, decodeErr
		}
		if applyErr := projector.repository.ApplyBlock(ctx, projector.scope, block, events); applyErr != nil {
			return cursor, applyErr
		}
	}
}

func (projector *Projector) commonAncestor(ctx context.Context, cursor Cursor) (uint64, error) {
	lowest := uint64(0)
	if cursor.Height > projector.scope.MaxReorgDepth {
		lowest = cursor.Height - projector.scope.MaxReorgDepth
	}
	for height := cursor.Height; ; height-- {
		canonical, exists, err := projector.repository.CanonicalHash(ctx, projector.scope, height)
		if err != nil {
			return 0, err
		}
		candidate, sourceErr := projector.source.Block(ctx, height)
		if sourceErr != nil {
			return 0, sourceErr
		}
		if exists && candidate.Hash == canonical {
			return height, nil
		}
		if height == lowest || height == 0 {
			break
		}
	}
	return 0, ErrReorgTooDeep
}

type Verifier struct {
	repository Repository
	scope      Scope
}

func NewVerifier(repository Repository, scope Scope) (*Verifier, error) {
	if repository == nil || !validScope(scope) {
		return nil, ErrInvalidInput
	}
	return &Verifier{repository: repository, scope: normalizeScope(scope)}, nil
}

func (verifier *Verifier) VerifySelection(ctx context.Context, transactionHash string) (selection.ChainResult, error) {
	if !validHash(strings.ToLower(transactionHash)) {
		return selection.ChainResult{}, selection.ErrInvalidInput
	}
	result, found, err := verifier.repository.SelectionResult(ctx, verifier.scope, strings.ToLower(transactionHash))
	if err != nil {
		return selection.ChainResult{}, err
	}
	if !found {
		return selection.ChainResult{Status: selection.ChainPending, TransactionHash: strings.ToLower(transactionHash)}, nil
	}
	return result, nil
}

type Reconciler struct {
	repository Repository
	inventory  InventorySource
	scope      Scope
	now        func() time.Time
}

func NewReconciler(repository Repository, inventory InventorySource, scope Scope) (*Reconciler, error) {
	if repository == nil || inventory == nil || !validScope(scope) {
		return nil, ErrInvalidInput
	}
	return &Reconciler{repository: repository, inventory: inventory, scope: normalizeScope(scope), now: func() time.Time { return time.Now().UTC() }}, nil
}

func (reconciler *Reconciler) Run(ctx context.Context) (ReconciliationRun, error) {
	cursor, err := reconciler.repository.Cursor(ctx, reconciler.scope)
	if err != nil || !cursor.Set {
		return ReconciliationRun{}, errors.Join(ErrPending, err)
	}
	expected, err := reconciler.repository.ExpectedInventory(ctx, reconciler.scope)
	if err != nil {
		return ReconciliationRun{}, err
	}
	observed, err := reconciler.inventory.ObservedInventory(ctx, reconciler.scope, cursor.Height, expected)
	if err != nil {
		return ReconciliationRun{}, err
	}
	differences := CompareInventory(expected, observed)
	now := reconciler.now()
	run := ReconciliationRun{ID: stableID("chain-reconciliation", reconciler.scope.ChainID, reconciler.scope.Contract, fmt.Sprint(cursor.Height), inventoryFingerprint(expected, observed)), Scope: reconciler.scope, SafeHeight: cursor.Height, Status: "matched", Differences: differences, StartedAt: now, FinishedAt: now}
	if len(differences) > 0 {
		run.Status = "difference_detected"
	}
	if err = reconciler.repository.RecordReconciliation(ctx, run); err != nil {
		return ReconciliationRun{}, err
	}
	return run, nil
}

func CompareInventory(expected, observed map[string]string) []Difference {
	keys := make(map[string]struct{}, len(expected)+len(observed))
	for key := range expected {
		keys[key] = struct{}{}
	}
	for key := range observed {
		keys[key] = struct{}{}
	}
	ordered := make([]string, 0, len(keys))
	for key := range keys {
		ordered = append(ordered, key)
	}
	sort.Strings(ordered)
	result := make([]Difference, 0)
	for _, key := range ordered {
		if expected[key] == observed[key] {
			continue
		}
		category, resource := "unknown", key
		if before, after, ok := strings.Cut(key, ":"); ok {
			category, resource = before, after
		}
		result = append(result, Difference{Category: category, ResourceID: resource, ExpectedValue: expected[key], ObservedValue: observed[key], Severity: "critical"})
	}
	return result
}

func inventoryFingerprint(expected, observed map[string]string) string {
	parts := make([]string, 0, len(expected)+len(observed))
	for key, value := range expected {
		parts = append(parts, "expected:"+key+"="+value)
	}
	for key, value := range observed {
		parts = append(parts, "observed:"+key+"="+value)
	}
	sort.Strings(parts)
	return stableID(parts...)
}

func validScope(scope Scope) bool {
	return validUnsigned(scope.ChainID) && validAddress(strings.ToLower(scope.Contract)) && scope.Confirmations > 0 && scope.MaxReorgDepth > 0
}

func normalizeScope(scope Scope) Scope {
	scope.Contract = strings.ToLower(scope.Contract)
	return scope
}

func validUnsigned(value string) bool {
	if value == "" || value == "0" || value[0] == '0' {
		return false
	}
	for _, character := range value {
		if character < '0' || character > '9' {
			return false
		}
	}
	return true
}

func validAddress(value string) bool {
	return len(value) == 42 && strings.HasPrefix(value, "0x") && validHex(value[2:], 20)
}
func validHash(value string) bool {
	return len(value) == 66 && strings.HasPrefix(value, "0x") && validHex(value[2:], 32)
}
func validHex(value string, size int) bool {
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == size && value == strings.ToLower(value)
}

func stableID(parts ...string) string {
	hash := sha256.New()
	for _, part := range parts {
		var size [8]byte
		binary.BigEndian.PutUint64(size[:], uint64(len(part)))
		_, _ = hash.Write(size[:])
		_, _ = hash.Write([]byte(part))
	}
	return "sha256:" + hex.EncodeToString(hash.Sum(nil))
}
