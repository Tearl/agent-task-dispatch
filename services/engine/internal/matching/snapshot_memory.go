package matching

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

type MemorySnapshotRepository struct {
	mu         sync.Mutex
	now        func() time.Time
	byTask     map[string][]Snapshot
	byIdentity map[string]Snapshot
}

func NewMemorySnapshotRepository() *MemorySnapshotRepository {
	return &MemorySnapshotRepository{
		now:        func() time.Time { return time.Now().UTC() },
		byTask:     make(map[string][]Snapshot),
		byIdentity: make(map[string]Snapshot),
	}
}

func (repository *MemorySnapshotRepository) Latest(_ context.Context, taskID, taskSpecHash, algorithmVersion string) (Snapshot, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	items := repository.byTask[taskID]
	for index := len(items) - 1; index >= 0; index-- {
		item := items[index]
		if item.TaskSpecHash == taskSpecHash && item.AlgorithmVersion == algorithmVersion {
			return cloneSnapshot(item), nil
		}
	}
	return Snapshot{}, ErrSnapshotNotFound
}

func (repository *MemorySnapshotRepository) CreateRevision(_ context.Context, key SnapshotKey, builder SnapshotBuilder) (Snapshot, bool, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	identity := snapshotIdentity(key)
	if existing, ok := repository.byIdentity[identity]; ok {
		return cloneSnapshot(existing), true, nil
	}
	revision := len(repository.byTask[key.TaskID]) + 1
	created, err := builder(revision)
	if err != nil {
		return Snapshot{}, false, err
	}
	created.CreatedAt = repository.now()
	created = cloneSnapshot(created)
	repository.byTask[key.TaskID] = append(repository.byTask[key.TaskID], created)
	repository.byIdentity[identity] = created
	return cloneSnapshot(created), false, nil
}

func snapshotIdentity(key SnapshotKey) string {
	encoded, err := json.Marshal(key)
	if err != nil {
		panic(fmt.Sprintf("encode snapshot identity: %v", err))
	}
	return string(encoded)
}

func cloneSnapshot(source Snapshot) Snapshot {
	encoded, err := json.Marshal(source)
	if err != nil {
		panic(fmt.Sprintf("clone matching snapshot: %v", err))
	}
	var result Snapshot
	if err = json.Unmarshal(encoded, &result); err != nil {
		panic(fmt.Sprintf("clone matching snapshot: %v", err))
	}
	return result
}
