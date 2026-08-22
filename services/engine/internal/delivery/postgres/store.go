package postgres

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/example/agent-platform/engine/internal/delivery"
	"github.com/example/agent-platform/engine/internal/execution"
	"github.com/example/agent-platform/engine/internal/persistence"
	"github.com/lib/pq"
)

type Store struct {
	db                 *sql.DB
	chainID            string
	contract           string
	asset              string
	platformIncidentID string
}

type scopeSource struct {
	TaskStatus, TaskSpecHash, AcceptanceHash, AcceptanceJSON string
	OverviewID, OverviewHash, OverviewRef                    string
	DeliveryFormat, Language, CostCap, AgentEndpoint         string
	AssignmentID, AgentID, ProviderID                        string
	Inputs, AllowedTools, Exclusions                         []string
	Deadline                                                 time.Time
	AssignmentWorkNonce                                      uint64
}

func NewStore(db *sql.DB) (*Store, error) {
	return NewStoreWithChain(db, "", "")
}

func NewStoreWithChain(db *sql.DB, chainID, contract string) (*Store, error) {
	return NewStoreWithConfig(db, chainID, contract, "", "")
}

func NewStoreWithConfig(db *sql.DB, chainID, contract, asset, platformIncidentID string) (*Store, error) {
	if db == nil {
		return nil, errors.New("database is required")
	}
	if (chainID == "") != (contract == "") {
		return nil, errors.New("chain id and contract must be configured together")
	}
	return &Store{db: db, chainID: chainID, contract: contract, asset: asset, platformIncidentID: platformIncidentID}, nil
}

func (store *Store) AuthorizeRevision(ctx context.Context, publisherID, taskID string, input delivery.StartInput) error {
	if store.chainID == "" || store.contract == "" {
		return delivery.ErrDependencyPending
	}
	return authorizeRevision(ctx, store.db, store.chainID, store.contract, publisherID, taskID, input)
}

func (store *Store) Start(ctx context.Context, mutation delivery.Mutation, taskID string, input delivery.StartInput) (delivery.StartResult, bool, error) {
	tx, err := store.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return delivery.StartResult{}, false, err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0)),pg_advisory_xact_lock(hashtextextended($2,0))`, "formal-start:"+mutation.PublisherID+":"+mutation.IdempotencyKey, "formal-task:"+taskID); err != nil {
		return delivery.StartResult{}, false, err
	}
	var storedHash string
	var storedBody []byte
	err = tx.QueryRowContext(ctx, `SELECT request_hash,response_body FROM formal_start_requests WHERE publisher_id=$1 AND idempotency_key=$2`, mutation.PublisherID, mutation.IdempotencyKey).Scan(&storedHash, &storedBody)
	if err == nil {
		if storedHash != mutation.RequestHash {
			return delivery.StartResult{}, false, persistence.ErrIdempotencyConflict
		}
		var replay delivery.StartResult
		if err = json.Unmarshal(storedBody, &replay); err != nil {
			return delivery.StartResult{}, false, err
		}
		if err = tx.Commit(); err != nil {
			return delivery.StartResult{}, false, err
		}
		return replay, true, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return delivery.StartResult{}, false, err
	}
	var owned bool
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM tasks WHERE task_id=$1 AND publisher_id=$2)`, taskID, mutation.PublisherID).Scan(&owned); err != nil {
		return delivery.StartResult{}, false, err
	}
	if !owned {
		return delivery.StartResult{}, false, delivery.ErrNotFound
	}
	source, err := loadScopeSource(ctx, tx, mutation.PublisherID, taskID)
	if errors.Is(err, sql.ErrNoRows) {
		return delivery.StartResult{}, false, delivery.ErrInvalidState
	}
	if err != nil {
		return delivery.StartResult{}, false, err
	}
	var now time.Time
	if err = tx.QueryRowContext(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
		return delivery.StartResult{}, false, err
	}
	if !source.Deadline.After(now) || source.AgentEndpoint == "" {
		return delivery.StartResult{}, false, delivery.ErrInvalidState
	}

	packageValue, packageErr := loadPackage(tx.QueryRowContext(ctx, packageSelect+` WHERE task_id=$1 AND publisher_id=$2`, taskID, mutation.PublisherID))
	var scope delivery.Scope
	if errors.Is(packageErr, sql.ErrNoRows) {
		if input.ExpectedPackageVersion != 0 || input.WorkNonce != source.AssignmentWorkNonce || input.WorkNonce != 1 || input.Revision != nil || source.TaskStatus != "assigned" {
			return delivery.StartResult{}, false, delivery.ErrInvalidState
		}
		packageID := digest("formal-package:" + source.AssignmentID + ":default:standard")
		scope, err = freezeScope(packageID, source, now)
		if err != nil {
			return delivery.StartResult{}, false, err
		}
		if err = insertScope(ctx, tx, scope); err != nil {
			return delivery.StartResult{}, false, err
		}
		packageValue = delivery.Package{ID: packageID, TaskID: taskID, AssignmentID: source.AssignmentID, DeliveryUnit: "default", Kind: delivery.StandardPackage, ScopeID: scope.ID, ScopeRevision: 1, AgentID: source.AgentID, ProviderID: source.ProviderID, PublisherID: mutation.PublisherID, IncludedVersions: delivery.IncludedVersions, MaximumVersions: delivery.MaximumVersions, AggregateVersion: 1, Status: delivery.PackageActive, CreatedAt: now, UpdatedAt: now}
		_, err = tx.ExecContext(ctx, `INSERT INTO formal_packages (package_id,protocol_version,task_id,assignment_id,delivery_unit,package_kind,scope_id,scope_revision,agent_id,provider_id,publisher_id,included_versions,maximum_versions,allocated_version,aggregate_version,status,created_at,updated_at) VALUES ($1,$2,$3,$4,$5,$6,$7,1,$8,$9,$10,3,5,0,1,'active',$11,$11)`, packageValue.ID, delivery.ProtocolVersion, packageValue.TaskID, packageValue.AssignmentID, packageValue.DeliveryUnit, packageValue.Kind, packageValue.ScopeID, packageValue.AgentID, packageValue.ProviderID, packageValue.PublisherID, now)
		if err != nil {
			return delivery.StartResult{}, false, err
		}
	} else if packageErr != nil {
		return delivery.StartResult{}, false, packageErr
	} else {
		if source.AssignmentID != packageValue.AssignmentID {
			return delivery.StartResult{}, false, delivery.ErrInvalidState
		}
		if source.TaskStatus != "formal_review" && source.TaskStatus != "revision_requested" {
			return delivery.StartResult{}, false, delivery.ErrInvalidState
		}
		scope, err = loadScope(tx.QueryRowContext(ctx, scopeSelect+` WHERE scope_id=$1`, packageValue.ScopeID))
		if err != nil {
			return delivery.StartResult{}, false, err
		}
	}
	if input.Revision != nil {
		if err = authorizeRevision(ctx, tx, store.chainID, store.contract, mutation.PublisherID, taskID, input); err != nil {
			return delivery.StartResult{}, false, err
		}
	}

	var previous *delivery.Version
	if packageValue.AllocatedVersion > 0 {
		loaded, loadErr := loadVersion(tx.QueryRowContext(ctx, versionSelect+` WHERE package_id=$1 AND version_no=$2 FOR UPDATE`, packageValue.ID, packageValue.AllocatedVersion))
		if loadErr != nil {
			return delivery.StartResult{}, false, loadErr
		}
		previous = &loaded
	}
	next, aggregateVersion, err := delivery.NextIncludedVersion(packageValue, previous, input)
	if err != nil {
		return delivery.StartResult{}, false, err
	}
	var active bool
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM formal_versions WHERE package_id=$1 AND status IN ('allocated','generating'))`, packageValue.ID).Scan(&active); err != nil {
		return delivery.StartResult{}, false, err
	}
	if active {
		return delivery.StartResult{}, false, delivery.ErrInvalidState
	}
	executionDeadline := source.Deadline
	changeResponsibility := ""
	if input.ChangeOrderID != "" {
		scope, executionDeadline, changeResponsibility, err = authorizeChangeOrder(ctx, tx, mutation.PublisherID, taskID, packageValue, next, input)
		if err != nil {
			return delivery.StartResult{}, false, err
		}
	}

	logicalExecutionID := digest(fmt.Sprintf("formal-execution:%s:%d", packageValue.ID, next))
	version := delivery.Version{PackageID: packageValue.ID, Number: next, AggregateVersion: aggregateVersion, ScopeID: scope.ID, ScopeHash: scope.ContentHash, WorkNonce: input.WorkNonce, Revision: input.Revision, ChangeOrderID: input.ChangeOrderID, LogicalExecutionID: logicalExecutionID, Status: delivery.VersionAllocated, UsedCost: "0", CreatedAt: now, UpdatedAt: now}
	if err = insertVersion(ctx, tx, version); err != nil {
		return delivery.StartResult{}, false, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE formal_packages SET allocated_version=$1,aggregate_version=$2,updated_at=$3 WHERE package_id=$4`, next, aggregateVersion, now, packageValue.ID); err != nil {
		return delivery.StartResult{}, false, err
	}
	if input.ChangeOrderID != "" {
		updated, updateErr := tx.ExecContext(ctx, `UPDATE formal_change_orders SET status='consumed',aggregate_version=aggregate_version+1,package_aggregate_version=$1,consumed_at=$2,updated_at=$2 WHERE change_order_id=$3 AND status='effective'`, aggregateVersion, now, input.ChangeOrderID)
		if updateErr != nil {
			return delivery.StartResult{}, false, updateErr
		}
		if err = requireOne(updated); err != nil {
			return delivery.StartResult{}, false, err
		}
		if err = insertChangeEvent(ctx, tx, input.ChangeOrderID, "consumed", mutation.PublisherID, map[string]any{"version": next, "workNonce": input.WorkNonce}, now); err != nil {
			return delivery.StartResult{}, false, err
		}
	}
	updatedTask, err := tx.ExecContext(ctx, `UPDATE tasks SET status='formal_generating',aggregate_version=aggregate_version+1,updated_at=$1 WHERE task_id=$2 AND status IN ('assigned','formal_review','revision_requested')`, now, taskID)
	if err != nil {
		return delivery.StartResult{}, false, err
	}
	if count, countErr := updatedTask.RowsAffected(); countErr != nil || count != 1 {
		if countErr != nil {
			return delivery.StartResult{}, false, countErr
		}
		return delivery.StartResult{}, false, delivery.ErrInvalidState
	}
	packageValue.AllocatedVersion, packageValue.AggregateVersion, packageValue.UpdatedAt = next, aggregateVersion, now
	if err = insertVersionEvent(ctx, tx, version, "allocated", "", map[string]any{"workNonce": input.WorkNonce, "scopeHash": scope.ContentHash}, now); err != nil {
		return delivery.StartResult{}, false, err
	}
	responsibilityCode := "included_package"
	if input.ChangeOrderID != "" {
		responsibilityCode = "change_order_" + changeResponsibility
	}
	command := execution.Spec{LogicalExecutionID: logicalExecutionID, Stage: execution.StageFormal, TaskID: taskID, TaskSpecHash: source.TaskSpecHash, InputRef: "formal-scope://" + scope.ID, InputHash: scope.ContentHash, AgentID: source.AgentID, AgentEndpoint: source.AgentEndpoint, ResponsibilityCode: responsibilityCode, CostCap: scope.ExternalCostCap, ToolPolicy: execution.ToolPolicy{Mode: execution.ToolPolicyScoped, AllowedTools: scope.AllowedTools}, Deadline: executionDeadline, IdempotencyKey: logicalExecutionID, Formal: &execution.FormalBinding{AssignmentID: source.AssignmentID, Package: packageValue.ID, Version: next, AggregateVersion: aggregateVersion, WorkNonce: int64(input.WorkNonce), ScopeSpecHash: scope.TaskSpecHash, ChangeOrderID: input.ChangeOrderID, Responsibility: changeResponsibility}}
	commandBody, err := json.Marshal(command)
	if err != nil {
		return delivery.StartResult{}, false, err
	}
	messageID := digest("outbox:" + logicalExecutionID)
	if _, err = tx.ExecContext(ctx, `INSERT INTO outbox_messages (message_id,dedupe_key,topic,payload,available_at,created_at) VALUES ($1,$2,'agent.execution.formal.requested',$3,$4,$4)`, messageID, logicalExecutionID, commandBody, now); err != nil {
		return delivery.StartResult{}, false, err
	}
	result := delivery.StartResult{Package: packageValue, Scope: scope, Version: version}
	responseBody, err := json.Marshal(result)
	if err != nil {
		return delivery.StartResult{}, false, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO formal_start_requests (publisher_id,idempotency_key,request_hash,task_id,package_id,version_no,response_body,created_at) VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`, mutation.PublisherID, mutation.IdempotencyKey, mutation.RequestHash, taskID, packageValue.ID, next, responseBody, now); err != nil {
		return delivery.StartResult{}, false, err
	}
	if err = tx.Commit(); err != nil {
		return delivery.StartResult{}, false, err
	}
	return result, false, nil
}

func (store *Store) SubmitFeedback(ctx context.Context, mutation delivery.Mutation, taskID string, input delivery.FeedbackInput, set delivery.FeedbackSet) (delivery.FeedbackSet, bool, error) {
	tx, err := store.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return delivery.FeedbackSet{}, false, err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0)),pg_advisory_xact_lock(hashtextextended($2,0))`, "formal-feedback:"+mutation.PublisherID+":"+mutation.IdempotencyKey, "formal-task:"+taskID); err != nil {
		return delivery.FeedbackSet{}, false, err
	}
	var storedHash string
	var storedBody []byte
	err = tx.QueryRowContext(ctx, `SELECT request_hash,response_body FROM formal_feedback_requests WHERE publisher_id=$1 AND idempotency_key=$2`, mutation.PublisherID, mutation.IdempotencyKey).Scan(&storedHash, &storedBody)
	if err == nil {
		if storedHash != mutation.RequestHash {
			return delivery.FeedbackSet{}, false, persistence.ErrIdempotencyConflict
		}
		var replay delivery.FeedbackSet
		if err = json.Unmarshal(storedBody, &replay); err != nil {
			return delivery.FeedbackSet{}, false, err
		}
		if err = tx.Commit(); err != nil {
			return delivery.FeedbackSet{}, false, err
		}
		return replay, true, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return delivery.FeedbackSet{}, false, err
	}
	packageValue, err := loadPackage(tx.QueryRowContext(ctx, packageSelect+` WHERE task_id=$1 AND publisher_id=$2 FOR UPDATE`, taskID, mutation.PublisherID))
	if errors.Is(err, sql.ErrNoRows) {
		return delivery.FeedbackSet{}, false, delivery.ErrNotFound
	}
	if err != nil {
		return delivery.FeedbackSet{}, false, err
	}
	if input.PackageID != packageValue.ID || input.ExpectedPackageVersion != packageValue.AggregateVersion || input.ParentVersion != packageValue.AllocatedVersion {
		return delivery.FeedbackSet{}, false, delivery.ErrStaleVersion
	}
	parent, err := loadVersion(tx.QueryRowContext(ctx, versionSelect+` WHERE package_id=$1 AND version_no=$2 FOR UPDATE`, packageValue.ID, input.ParentVersion))
	if err != nil {
		return delivery.FeedbackSet{}, false, err
	}
	if parent.Status != delivery.VersionReview || parent.ContentHash != input.ParentContentHash || packageValue.AllocatedVersion >= packageValue.MaximumVersions {
		return delivery.FeedbackSet{}, false, delivery.ErrInvalidState
	}
	var taskStatus string
	if err = tx.QueryRowContext(ctx, `SELECT status FROM tasks WHERE task_id=$1 FOR UPDATE`, taskID).Scan(&taskStatus); err != nil {
		return delivery.FeedbackSet{}, false, err
	}
	if taskStatus != "formal_review" {
		return delivery.FeedbackSet{}, false, delivery.ErrInvalidState
	}
	var now time.Time
	if err = tx.QueryRowContext(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
		return delivery.FeedbackSet{}, false, err
	}
	set.PackageID, set.ScopeID, set.ScopeHash, set.PackageAggregateVersion, set.CreatedAt = packageValue.ID, parent.ScopeID, parent.ScopeHash, packageValue.AggregateVersion+1, now
	if _, err = tx.ExecContext(ctx, `INSERT INTO formal_feedback_sets (feedback_set_id,feedback_version,package_id,parent_version,parent_content_hash,scope_id,scope_hash,feedback_digest,package_aggregate_version,created_at) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, set.ID, delivery.FeedbackVersion, set.PackageID, set.ParentVersion, set.ParentContentHash, set.ScopeID, set.ScopeHash, set.Digest, set.PackageAggregateVersion, now); err != nil {
		return delivery.FeedbackSet{}, false, formalConflict(err)
	}
	for _, item := range set.Items {
		if _, err = tx.ExecContext(ctx, `INSERT INTO formal_feedback_items (feedback_set_id,feedback_item_id,ordinal,criterion_id,category,priority,target,description,expected_outcome,scope_claim) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, set.ID, item.ID, item.Ordinal, item.CriterionID, item.Category, item.Priority, item.Target, item.Description, item.ExpectedOutcome, item.ScopeClaim); err != nil {
			return delivery.FeedbackSet{}, false, err
		}
	}
	if _, err = tx.ExecContext(ctx, `UPDATE formal_packages SET aggregate_version=$1,updated_at=$2 WHERE package_id=$3`, set.PackageAggregateVersion, now, packageValue.ID); err != nil {
		return delivery.FeedbackSet{}, false, err
	}
	updated, updateErr := tx.ExecContext(ctx, `UPDATE tasks SET status='revision_requested',aggregate_version=aggregate_version+1,updated_at=$1 WHERE task_id=$2 AND status='formal_review'`, now, taskID)
	if updateErr != nil {
		return delivery.FeedbackSet{}, false, updateErr
	}
	if err = requireOne(updated); err != nil {
		return delivery.FeedbackSet{}, false, err
	}
	responseBody, err := json.Marshal(set)
	if err != nil {
		return delivery.FeedbackSet{}, false, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO formal_feedback_requests (publisher_id,idempotency_key,request_hash,task_id,feedback_set_id,response_body,created_at) VALUES ($1,$2,$3,$4,$5,$6,$7)`, mutation.PublisherID, mutation.IdempotencyKey, mutation.RequestHash, taskID, set.ID, responseBody, now); err != nil {
		return delivery.FeedbackSet{}, false, err
	}
	if err = tx.Commit(); err != nil {
		return delivery.FeedbackSet{}, false, err
	}
	return set, false, nil
}

func (store *Store) Get(ctx context.Context, publisherID, taskID string) (delivery.View, error) {
	packageValue, err := loadPackage(store.db.QueryRowContext(ctx, packageSelect+` WHERE task_id=$1 AND publisher_id=$2`, taskID, publisherID))
	if errors.Is(err, sql.ErrNoRows) {
		return delivery.View{}, delivery.ErrNotFound
	}
	if err != nil {
		return delivery.View{}, err
	}
	scope, err := loadScope(store.db.QueryRowContext(ctx, scopeSelect+` WHERE scope_id=$1`, packageValue.ScopeID))
	if err != nil {
		return delivery.View{}, err
	}
	rows, err := store.db.QueryContext(ctx, versionSelect+` WHERE package_id=$1 ORDER BY version_no`, packageValue.ID)
	if err != nil {
		return delivery.View{}, err
	}
	defer rows.Close()
	versions := make([]delivery.Version, 0, packageValue.AllocatedVersion)
	for rows.Next() {
		value, scanErr := loadVersion(rows)
		if scanErr != nil {
			return delivery.View{}, scanErr
		}
		versions = append(versions, value)
	}
	if err = rows.Err(); err != nil {
		return delivery.View{}, err
	}
	for index := range versions {
		if err = store.loadVersionDetails(ctx, &versions[index]); err != nil {
			return delivery.View{}, err
		}
	}
	feedback, err := store.loadFeedback(ctx, packageValue.ID)
	if err != nil {
		return delivery.View{}, err
	}
	changeOrders, err := store.loadChangeOrders(ctx, packageValue.ID)
	if err != nil {
		return delivery.View{}, err
	}
	return delivery.View{Package: packageValue, Scope: scope, Versions: versions, Feedback: feedback, ChangeOrders: changeOrders}, nil
}

func (store *Store) ProofContext(ctx context.Context, logicalExecutionID string) (delivery.ProofContext, error) {
	var value delivery.ProofContext
	var parentHash, feedbackDigest sql.NullString
	err := store.db.QueryRowContext(ctx, `SELECT package.task_id,package.assignment_id,package.delivery_unit,version.package_id,version.scope_hash,version.version_no,version.package_aggregate_version,version.work_nonce,package.agent_id,version.parent_content_hash,version.feedback_digest,COALESCE(version.change_order_id,''),version.scope_hash,COALESCE(change_order.deadline,task.deadline)
FROM formal_versions version JOIN formal_packages package ON package.package_id=version.package_id JOIN tasks task ON task.task_id=package.task_id LEFT JOIN formal_change_orders change_order ON change_order.change_order_id=version.change_order_id
WHERE version.logical_execution_id=$1`, logicalExecutionID).Scan(&value.TaskID, &value.AssignmentID, &value.DeliveryUnit, &value.PackageID, &value.ScopeHash, &value.Version, &value.PackageAggregateVersion, &value.WorkNonce, &value.AgentID, &parentHash, &feedbackDigest, &value.ChangeOrderID, &value.PolicyHash, &value.Deadline)
	if errors.Is(err, sql.ErrNoRows) {
		return value, delivery.ErrNotFound
	}
	if err != nil {
		return value, err
	}
	value.ParentContentHash, value.FeedbackDigest = parentHash.String, feedbackDigest.String
	rows, err := store.db.QueryContext(ctx, `SELECT item.feedback_item_id FROM formal_versions version JOIN formal_feedback_items item ON item.feedback_set_id=version.feedback_set_id WHERE version.logical_execution_id=$1 ORDER BY item.ordinal`, logicalExecutionID)
	if err != nil {
		return value, err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			return value, err
		}
		value.FeedbackItemIDs = append(value.FeedbackItemIDs, id)
	}
	return value, rows.Err()
}

func (store *Store) RecordDispatched(ctx context.Context, logicalExecutionID string) (delivery.Version, bool, error) {
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return delivery.Version{}, false, err
	}
	defer func() { _ = tx.Rollback() }()
	value, err := loadVersion(tx.QueryRowContext(ctx, versionSelect+` WHERE logical_execution_id=$1 FOR UPDATE`, logicalExecutionID))
	if errors.Is(err, sql.ErrNoRows) {
		return delivery.Version{}, false, delivery.ErrNotFound
	}
	if err != nil {
		return delivery.Version{}, false, err
	}
	if value.Status == delivery.VersionGenerating {
		_ = tx.Commit()
		return value, true, nil
	}
	if value.Status != delivery.VersionAllocated {
		return delivery.Version{}, false, delivery.ErrInvalidState
	}
	active, err := activePackageAssignment(ctx, tx, value.PackageID)
	if err != nil {
		return delivery.Version{}, false, err
	}
	if !active {
		return delivery.Version{}, false, delivery.ErrInvalidState
	}
	var now time.Time
	if err = tx.QueryRowContext(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
		return delivery.Version{}, false, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE formal_versions SET status='generating',updated_at=$1 WHERE logical_execution_id=$2`, now, logicalExecutionID); err != nil {
		return delivery.Version{}, false, err
	}
	value.Status, value.UpdatedAt = delivery.VersionGenerating, now
	if err = insertVersionEvent(ctx, tx, value, "generating", "", map[string]any{}, now); err != nil {
		return delivery.Version{}, false, err
	}
	if err = tx.Commit(); err != nil {
		return delivery.Version{}, false, err
	}
	return value, false, nil
}

func (store *Store) RecordResult(ctx context.Context, result delivery.ExecutionResult, proof *delivery.ProofRecord) (delivery.Version, bool, error) {
	resultBody, marshalErr := json.Marshal(struct {
		Result delivery.ExecutionResult `json:"result"`
		Proof  *delivery.ProofRecord    `json:"proof,omitempty"`
	}{result, proof})
	if marshalErr != nil {
		return delivery.Version{}, false, marshalErr
	}
	resultHash := digest(string(resultBody))
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return delivery.Version{}, false, err
	}
	defer func() { _ = tx.Rollback() }()
	value, err := loadVersion(tx.QueryRowContext(ctx, versionSelect+` WHERE logical_execution_id=$1 FOR UPDATE`, result.LogicalExecutionID))
	if errors.Is(err, sql.ErrNoRows) {
		return delivery.Version{}, false, delivery.ErrNotFound
	}
	if err != nil {
		return delivery.Version{}, false, err
	}
	if value.Status == delivery.VersionReview || value.Status == delivery.VersionFailed {
		if value.ResultHash == resultHash {
			_ = tx.Commit()
			return value, true, nil
		}
		return delivery.Version{}, false, delivery.ErrContentConflict
	}
	if value.Status != delivery.VersionAllocated && value.Status != delivery.VersionGenerating {
		return delivery.Version{}, false, delivery.ErrInvalidState
	}
	active, err := activePackageAssignment(ctx, tx, value.PackageID)
	if err != nil {
		return delivery.Version{}, false, err
	}
	if !active {
		return delivery.Version{}, false, delivery.ErrInvalidState
	}
	var now time.Time
	if err = tx.QueryRowContext(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
		return delivery.Version{}, false, err
	}
	status, reason := delivery.VersionFailed, result.FailureReasonCode
	if result.Status == delivery.ResultSucceeded {
		var duplicate bool
		if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM formal_versions WHERE package_id=$1 AND version_no<>$2 AND status='review' AND content_hash=$3)`, value.PackageID, value.Number, result.ContentHash).Scan(&duplicate); err != nil {
			return delivery.Version{}, false, err
		}
		if duplicate {
			reason = "duplicate_content"
		} else {
			status = delivery.VersionReview
		}
	}
	if status == delivery.VersionReview {
		if proof == nil || proof.Proof.PackageID != value.PackageID || proof.Proof.FormalVersion != value.Number || proof.Proof.ContentHash != result.ContentHash || proof.Proof.PackageAggregateVersion != value.AggregateVersion || proof.Proof.WorkNonce != value.WorkNonce {
			return delivery.Version{}, false, delivery.ErrInvalidInput
		}
		proofBody, proofErr := json.Marshal(proof.Proof)
		if proofErr != nil {
			return delivery.Version{}, false, proofErr
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO formal_delivery_proofs (package_id,version_no,proof_version,proof_body,payload_hash,proof_digest,signature,created_at) VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`, value.PackageID, value.Number, proof.Proof.Version, proofBody, proof.PayloadHash, proof.Digest, proof.Signature, now); err != nil {
			return delivery.Version{}, false, err
		}
		for _, response := range result.FeedbackResponses {
			if _, err = tx.ExecContext(ctx, `INSERT INTO formal_feedback_responses (package_id,version_no,feedback_item_id,disposition,summary) VALUES ($1,$2,$3,$4,$5)`, value.PackageID, value.Number, response.FeedbackItemID, response.Disposition, response.Summary); err != nil {
				return delivery.Version{}, false, err
			}
		}
		for index, change := range result.Changes {
			if _, err = tx.ExecContext(ctx, `INSERT INTO formal_version_changes (package_id,version_no,ordinal,path,change_kind,before_hash,after_hash) VALUES ($1,$2,$3,$4,$5,$6,$7)`, value.PackageID, value.Number, index+1, change.Path, change.Kind, nullableString(change.BeforeHash), nullableString(change.AfterHash)); err != nil {
				return delivery.Version{}, false, err
			}
		}
		if _, err = tx.ExecContext(ctx, `UPDATE formal_versions SET status='review',content_hash=$1,deliverable_ref=$2,used_cost=$3,result_hash=$4,updated_at=$5 WHERE logical_execution_id=$6`, result.ContentHash, result.DeliverableRef, result.UsedCost, resultHash, now, result.LogicalExecutionID); err != nil {
			return delivery.Version{}, false, err
		}
		value.Status, value.ContentHash, value.DeliverableRef, value.UsedCost, value.ResultHash, value.UpdatedAt, value.FeedbackResponses, value.Changes, value.Proof = status, result.ContentHash, result.DeliverableRef, result.UsedCost, resultHash, now, result.FeedbackResponses, result.Changes, proof
		billingKey := digest(fmt.Sprintf("formal-billing:%s:%d", value.PackageID, value.Number))
		billingStatus, chargeAmount := "included", "0"
		if value.ChangeOrderID != "" {
			billingStatus = "change_order"
			if err = tx.QueryRowContext(ctx, `SELECT authorized_price::text FROM formal_change_orders WHERE change_order_id=$1 AND status='consumed'`, value.ChangeOrderID).Scan(&chargeAmount); err != nil {
				return delivery.Version{}, false, err
			}
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO formal_billing_results (package_id,version_no,billing_key,billing_status,charge_amount,used_cost,content_hash,created_at) VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`, value.PackageID, value.Number, billingKey, billingStatus, chargeAmount, result.UsedCost, result.ContentHash, now); err != nil {
			return delivery.Version{}, false, err
		}
		updated, updateErr := tx.ExecContext(ctx, `UPDATE tasks SET status='formal_review',aggregate_version=aggregate_version+1,updated_at=$1 WHERE task_id=(SELECT task_id FROM formal_packages WHERE package_id=$2) AND status='formal_generating'`, now, value.PackageID)
		if updateErr != nil {
			return delivery.Version{}, false, updateErr
		}
		if err = requireOne(updated); err != nil {
			return delivery.Version{}, false, err
		}
	} else {
		if reason == "" {
			reason = "execution_failed"
		}
		if _, err = tx.ExecContext(ctx, `UPDATE formal_versions SET status='failed',used_cost=$1,failure_reason_code=$2,result_hash=$3,updated_at=$4 WHERE logical_execution_id=$5`, result.UsedCost, reason, resultHash, now, result.LogicalExecutionID); err != nil {
			return delivery.Version{}, false, err
		}
		value.Status, value.UsedCost, value.FailureReasonCode, value.ResultHash, value.UpdatedAt = status, result.UsedCost, reason, resultHash, now
		updated, updateErr := tx.ExecContext(ctx, `UPDATE tasks SET status='failed',aggregate_version=aggregate_version+1,updated_at=$1 WHERE task_id=(SELECT task_id FROM formal_packages WHERE package_id=$2) AND status='formal_generating'`, now, value.PackageID)
		if updateErr != nil {
			return delivery.Version{}, false, updateErr
		}
		if err = requireOne(updated); err != nil {
			return delivery.Version{}, false, err
		}
	}
	if err = insertVersionEvent(ctx, tx, value, value.Status, reason, map[string]any{"contentHash": value.ContentHash, "usedCost": value.UsedCost}, now); err != nil {
		return delivery.Version{}, false, err
	}
	if err = tx.Commit(); err != nil {
		return delivery.Version{}, false, err
	}
	return value, false, nil
}

func loadScopeSource(ctx context.Context, tx *sql.Tx, publisherID, taskID string) (scopeSource, error) {
	var value scopeSource
	err := tx.QueryRowContext(ctx, `SELECT t.status,t.deadline,spec.content_hash,spec.inputs,spec.allowed_tools,spec.exclusions,spec.delivery_format,spec.language,
acceptance.content_hash,acceptance.criteria,assignment.assignment_id,assignment.agent_id,assignment.provider_id,assignment.work_nonce,
reservation.slot_id,slot.content_hash,slot.deliverable_ref,LEAST(t.external_cost_cap,price.external_cost_cap)::text,agent.endpoint_url
FROM tasks t
JOIN task_spec_versions spec ON spec.task_id=t.task_id AND spec.version_no=t.current_spec_version
JOIN acceptance_versions acceptance ON acceptance.task_id=t.task_id AND acceptance.version_no=t.current_acceptance_version
JOIN active_assignments active ON active.task_id=t.task_id
JOIN assignments assignment ON assignment.assignment_id=active.assignment_id
JOIN selection_reservations reservation ON reservation.reservation_id=assignment.reservation_id AND reservation.status='confirmed'
JOIN overview_slots slot ON slot.slot_id=reservation.slot_id AND slot.status='valid' AND slot.billing_status='captured'
JOIN agent_price_versions price ON price.agent_id=assignment.agent_id AND price.version_no=reservation.price_version
JOIN agents agent ON agent.agent_id=assignment.agent_id
WHERE t.task_id=$1 AND t.publisher_id=$2 FOR UPDATE OF t,assignment`, taskID, publisherID).Scan(&value.TaskStatus, &value.Deadline, &value.TaskSpecHash, pq.Array(&value.Inputs), pq.Array(&value.AllowedTools), pq.Array(&value.Exclusions), &value.DeliveryFormat, &value.Language, &value.AcceptanceHash, &value.AcceptanceJSON, &value.AssignmentID, &value.AgentID, &value.ProviderID, &value.AssignmentWorkNonce, &value.OverviewID, &value.OverviewHash, &value.OverviewRef, &value.CostCap, &value.AgentEndpoint)
	return value, err
}

func activePackageAssignment(ctx context.Context, tx *sql.Tx, packageID string) (bool, error) {
	var active bool
	err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM formal_packages package JOIN active_assignments assignment ON assignment.task_id=package.task_id AND assignment.assignment_id=package.assignment_id WHERE package.package_id=$1)`, packageID).Scan(&active)
	return active, err
}

func requireOne(result sql.Result) error {
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return delivery.ErrInvalidState
	}
	return nil
}

func freezeScope(packageID string, source scopeSource, now time.Time) (delivery.Scope, error) {
	criteria := make([]map[string]any, 0)
	if err := json.Unmarshal([]byte(source.AcceptanceJSON), &criteria); err != nil {
		return delivery.Scope{}, err
	}
	output := map[string]any{"format": source.DeliveryFormat, "quantity": 1, "language": source.Language}
	body := map[string]any{"taskSpecHash": source.TaskSpecHash, "selectedOverviewId": source.OverviewID, "overviewContentHash": source.OverviewHash, "overviewRef": source.OverviewRef, "inputs": source.Inputs, "acceptanceHash": source.AcceptanceHash, "acceptanceCriteria": criteria, "outputConstraints": output, "allowedTools": source.AllowedTools, "externalCostCap": source.CostCap, "exclusions": source.Exclusions}
	encoded, err := json.Marshal(body)
	if err != nil {
		return delivery.Scope{}, err
	}
	contentHash := digest(string(encoded))
	return delivery.Scope{ID: digest("formal-scope:" + packageID + ":" + contentHash), PackageID: packageID, Revision: 1, ContentHash: contentHash, TaskSpecHash: source.TaskSpecHash, SelectedOverviewID: source.OverviewID, OverviewHash: source.OverviewHash, OverviewRef: source.OverviewRef, Inputs: source.Inputs, AcceptanceHash: source.AcceptanceHash, AcceptanceCriteria: criteria, OutputConstraints: output, AllowedTools: source.AllowedTools, ExternalCostCap: source.CostCap, Exclusions: source.Exclusions, CreatedAt: now}, nil
}

func insertScope(ctx context.Context, tx *sql.Tx, value delivery.Scope) error {
	criteria, _ := json.Marshal(value.AcceptanceCriteria)
	output, _ := json.Marshal(value.OutputConstraints)
	body, _ := json.Marshal(map[string]any{"taskSpecHash": value.TaskSpecHash, "selectedOverviewId": value.SelectedOverviewID, "overviewContentHash": value.OverviewHash, "overviewRef": value.OverviewRef, "inputs": value.Inputs, "acceptanceHash": value.AcceptanceHash, "acceptanceCriteria": value.AcceptanceCriteria, "outputConstraints": value.OutputConstraints, "allowedTools": value.AllowedTools, "externalCostCap": value.ExternalCostCap, "exclusions": value.Exclusions})
	_, err := tx.ExecContext(ctx, `INSERT INTO formal_scope_snapshots (scope_id,package_id,scope_revision,scope_version,content_hash,task_spec_hash,selected_overview_id,overview_content_hash,overview_ref,input_snapshot,acceptance_hash,acceptance_criteria,output_constraints,allowed_tools,external_cost_cap,exclusions,scope_body,created_at) VALUES ($1,$2,1,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17)`, value.ID, value.PackageID, delivery.ScopeVersion, value.ContentHash, value.TaskSpecHash, value.SelectedOverviewID, value.OverviewHash, value.OverviewRef, pq.Array(value.Inputs), value.AcceptanceHash, criteria, output, pq.Array(value.AllowedTools), value.ExternalCostCap, pq.Array(value.Exclusions), body, value.CreatedAt)
	return err
}

func insertVersion(ctx context.Context, tx *sql.Tx, value delivery.Version) error {
	var parent any
	var parentHash, feedbackID, feedbackDigest, feedbackAggregate any
	if value.Revision != nil {
		parent, parentHash, feedbackID, feedbackDigest = value.Revision.ParentVersion, value.Revision.ParentContentHash, value.Revision.FeedbackSetID, value.Revision.FeedbackDigest
		feedbackAggregate = value.Revision.FeedbackAggregateVersion
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO formal_versions (package_id,version_no,package_aggregate_version,scope_id,scope_hash,work_nonce,parent_version,parent_content_hash,feedback_set_id,feedback_digest,feedback_aggregate_version,change_order_id,logical_execution_id,status,created_at,updated_at) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,'allocated',$14,$14)`, value.PackageID, value.Number, value.AggregateVersion, value.ScopeID, value.ScopeHash, value.WorkNonce, parent, parentHash, feedbackID, feedbackDigest, feedbackAggregate, nullableString(value.ChangeOrderID), value.LogicalExecutionID, value.CreatedAt)
	return err
}

func insertVersionEvent(ctx context.Context, tx *sql.Tx, value delivery.Version, eventType, reason string, payload any, now time.Time) error {
	var sequence int64
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(max(event_sequence),0)+1 FROM formal_version_events WHERE package_id=$1 AND version_no=$2`, value.PackageID, value.Number).Scan(&sequence); err != nil {
		return err
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	var reasonValue any
	if reason != "" {
		reasonValue = reason
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO formal_version_events (event_id,package_id,version_no,event_sequence,event_type,reason_code,payload,occurred_at) VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`, digest(fmt.Sprintf("formal-event:%s:%d:%d:%s", value.PackageID, value.Number, sequence, eventType)), value.PackageID, value.Number, sequence, eventType, reasonValue, body, now)
	return err
}

func digest(value string) string {
	sum := sha256.Sum256([]byte(value))
	return "sha256:" + hex.EncodeToString(sum[:])
}

const packageSelect = `SELECT package_id,task_id,assignment_id,delivery_unit,package_kind,scope_id,scope_revision,agent_id,provider_id,publisher_id,included_versions,maximum_versions,allocated_version,aggregate_version,status,created_at,updated_at FROM formal_packages`
const scopeSelect = `SELECT scope_id,package_id,scope_revision,content_hash,task_spec_hash,selected_overview_id,overview_content_hash,overview_ref,input_snapshot,acceptance_hash,acceptance_criteria,output_constraints,allowed_tools,external_cost_cap::text,exclusions,COALESCE(change_order_id,''),scope_differences,created_at FROM formal_scope_snapshots`
const versionSelect = `SELECT package_id,version_no,package_aggregate_version,scope_id,scope_hash,work_nonce,parent_version,parent_content_hash,feedback_set_id,feedback_digest,feedback_aggregate_version,COALESCE(change_order_id,''),logical_execution_id,status,content_hash,deliverable_ref,used_cost::text,failure_reason_code,result_hash,created_at,updated_at FROM formal_versions`

type scanner interface{ Scan(...any) error }

func loadPackage(row scanner) (delivery.Package, error) {
	var value delivery.Package
	err := row.Scan(&value.ID, &value.TaskID, &value.AssignmentID, &value.DeliveryUnit, &value.Kind, &value.ScopeID, &value.ScopeRevision, &value.AgentID, &value.ProviderID, &value.PublisherID, &value.IncludedVersions, &value.MaximumVersions, &value.AllocatedVersion, &value.AggregateVersion, &value.Status, &value.CreatedAt, &value.UpdatedAt)
	return value, err
}

func loadScope(row scanner) (delivery.Scope, error) {
	var value delivery.Scope
	var criteria, output, differences []byte
	err := row.Scan(&value.ID, &value.PackageID, &value.Revision, &value.ContentHash, &value.TaskSpecHash, &value.SelectedOverviewID, &value.OverviewHash, &value.OverviewRef, pq.Array(&value.Inputs), &value.AcceptanceHash, &criteria, &output, pq.Array(&value.AllowedTools), &value.ExternalCostCap, pq.Array(&value.Exclusions), &value.ChangeOrderID, &differences, &value.CreatedAt)
	if err == nil {
		err = json.Unmarshal(criteria, &value.AcceptanceCriteria)
	}
	if err == nil {
		err = json.Unmarshal(output, &value.OutputConstraints)
	}
	if err == nil {
		err = json.Unmarshal(differences, &value.Differences)
	}
	return value, err
}

func loadVersion(row scanner) (delivery.Version, error) {
	var value delivery.Version
	var parent sql.NullInt64
	var feedbackAggregate sql.NullInt64
	var parentHash, feedbackID, feedbackDigest, contentHash, deliverableRef, reason, resultHash sql.NullString
	err := row.Scan(&value.PackageID, &value.Number, &value.AggregateVersion, &value.ScopeID, &value.ScopeHash, &value.WorkNonce, &parent, &parentHash, &feedbackID, &feedbackDigest, &feedbackAggregate, &value.ChangeOrderID, &value.LogicalExecutionID, &value.Status, &contentHash, &deliverableRef, &value.UsedCost, &reason, &resultHash, &value.CreatedAt, &value.UpdatedAt)
	if err != nil {
		return value, err
	}
	if parent.Valid {
		value.Revision = &delivery.RevisionBinding{ParentVersion: int(parent.Int64), ParentContentHash: parentHash.String, FeedbackSetID: feedbackID.String, FeedbackDigest: feedbackDigest.String, FeedbackAggregateVersion: feedbackAggregate.Int64}
	}
	value.ContentHash, value.DeliverableRef, value.FailureReasonCode, value.ResultHash = contentHash.String, deliverableRef.String, reason.String, resultHash.String
	return value, nil
}

type queryRower interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func authorizeRevision(ctx context.Context, query queryRower, chainID, contract, publisherID, taskID string, input delivery.StartInput) error {
	if input.Revision == nil {
		return nil
	}
	if chainID == "" || contract == "" {
		return delivery.ErrDependencyPending
	}
	var feedbackCreated time.Time
	var valid bool
	err := query.QueryRowContext(ctx, `SELECT GREATEST(feedback.created_at,COALESCE((SELECT effective_at FROM formal_change_orders WHERE change_order_id=NULLIF($8,'')),feedback.created_at)),
feedback.parent_version=$4 AND feedback.parent_content_hash=$5 AND feedback.feedback_digest=$7
AND feedback.scope_id=(SELECT scope_id FROM formal_versions WHERE package_id=package.package_id AND version_no=$4) AND (
  ($8='' AND feedback.package_aggregate_version=$6 AND package.aggregate_version=$6
    AND NOT EXISTS (SELECT 1 FROM formal_feedback_items item WHERE item.feedback_set_id=feedback.feedback_set_id AND item.scope_claim<>'in_scope'))
  OR ($8<>'' AND EXISTS (SELECT 1 FROM formal_change_orders change_order WHERE change_order.change_order_id=$8
    AND change_order.package_id=package.package_id AND change_order.feedback_set_id=feedback.feedback_set_id
    AND change_order.feedback_digest=feedback.feedback_digest AND change_order.status='effective'
    AND change_order.package_aggregate_version=package.aggregate_version)))
FROM formal_feedback_sets feedback JOIN formal_packages package ON package.package_id=feedback.package_id
WHERE package.task_id=$1 AND package.publisher_id=$2 AND feedback.feedback_set_id=$3 AND package.allocated_version=$4`, taskID, publisherID, input.Revision.FeedbackSetID, input.Revision.ParentVersion, input.Revision.ParentContentHash, input.Revision.FeedbackAggregateVersion, input.Revision.FeedbackDigest, input.ChangeOrderID).Scan(&feedbackCreated, &valid)
	if errors.Is(err, sql.ErrNoRows) || !valid {
		return delivery.ErrStaleVersion
	}
	if err != nil {
		return err
	}
	var confirmed bool
	err = query.QueryRowContext(ctx, `SELECT EXISTS(
SELECT 1 FROM chain_events event
JOIN chain_canonical_blocks canonical ON canonical.chain_id=event.chain_id AND canonical.contract_address=event.contract_address AND canonical.block_hash=event.block_hash
JOIN chain_blocks block ON block.chain_id=event.chain_id AND block.contract_address=event.contract_address AND block.block_hash=event.block_hash
JOIN formal_packages package ON package.task_id=$3 AND package.publisher_id=$4
JOIN assignments assignment ON assignment.assignment_id=package.assignment_id
JOIN selection_reservations reservation ON reservation.reservation_id=assignment.reservation_id
WHERE event.chain_id=$1 AND event.contract_address=$2 AND event.event_type='work_nonce_advanced'
AND event.task_chain_id=reservation.proof_task_id AND event.assignment_chain_id=assignment.assignment_id
AND COALESCE(event.work_nonce,(event.payload->>'workNonce')::bigint)=$5 AND block.block_timestamp >= $6
AND NOT EXISTS (SELECT 1 FROM chain_events newer JOIN chain_canonical_blocks c ON c.chain_id=newer.chain_id AND c.contract_address=newer.contract_address AND c.block_hash=newer.block_hash WHERE newer.chain_id=event.chain_id AND newer.contract_address=event.contract_address AND newer.event_type='work_nonce_advanced' AND newer.task_chain_id=event.task_chain_id AND COALESCE(newer.work_nonce,(newer.payload->>'workNonce')::bigint)>COALESCE(event.work_nonce,(event.payload->>'workNonce')::bigint)))`, chainID, contract, taskID, publisherID, input.WorkNonce, feedbackCreated).Scan(&confirmed)
	if err != nil {
		return err
	}
	if !confirmed {
		return delivery.ErrDependencyPending
	}
	return nil
}

func (store *Store) loadFeedback(ctx context.Context, packageID string) ([]delivery.FeedbackSet, error) {
	rows, err := store.db.QueryContext(ctx, `SELECT feedback_set_id,package_id,parent_version,parent_content_hash,scope_id,scope_hash,feedback_digest,package_aggregate_version,created_at FROM formal_feedback_sets WHERE package_id=$1 ORDER BY package_aggregate_version`, packageID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	values := make([]delivery.FeedbackSet, 0)
	for rows.Next() {
		var value delivery.FeedbackSet
		if err = rows.Scan(&value.ID, &value.PackageID, &value.ParentVersion, &value.ParentContentHash, &value.ScopeID, &value.ScopeHash, &value.Digest, &value.PackageAggregateVersion, &value.CreatedAt); err != nil {
			return nil, err
		}
		itemRows, itemErr := store.db.QueryContext(ctx, `SELECT feedback_item_id,ordinal,criterion_id,category,priority,target,description,expected_outcome,scope_claim FROM formal_feedback_items WHERE feedback_set_id=$1 ORDER BY ordinal`, value.ID)
		if itemErr != nil {
			return nil, itemErr
		}
		for itemRows.Next() {
			var item delivery.FeedbackItem
			if itemErr = itemRows.Scan(&item.ID, &item.Ordinal, &item.CriterionID, &item.Category, &item.Priority, &item.Target, &item.Description, &item.ExpectedOutcome, &item.ScopeClaim); itemErr != nil {
				_ = itemRows.Close()
				return nil, itemErr
			}
			value.Items = append(value.Items, item)
		}
		if itemErr = itemRows.Close(); itemErr != nil {
			return nil, itemErr
		}
		values = append(values, value)
	}
	return values, rows.Err()
}

func (store *Store) loadVersionDetails(ctx context.Context, value *delivery.Version) error {
	responseRows, err := store.db.QueryContext(ctx, `SELECT feedback_item_id,disposition,summary FROM formal_feedback_responses WHERE package_id=$1 AND version_no=$2 ORDER BY feedback_item_id`, value.PackageID, value.Number)
	if err != nil {
		return err
	}
	for responseRows.Next() {
		var response delivery.FeedbackResponse
		if err = responseRows.Scan(&response.FeedbackItemID, &response.Disposition, &response.Summary); err != nil {
			_ = responseRows.Close()
			return err
		}
		value.FeedbackResponses = append(value.FeedbackResponses, response)
	}
	if err = responseRows.Close(); err != nil {
		return err
	}
	changeRows, err := store.db.QueryContext(ctx, `SELECT path,change_kind,COALESCE(before_hash,''),COALESCE(after_hash,'') FROM formal_version_changes WHERE package_id=$1 AND version_no=$2 ORDER BY ordinal`, value.PackageID, value.Number)
	if err != nil {
		return err
	}
	for changeRows.Next() {
		var change delivery.Change
		if err = changeRows.Scan(&change.Path, &change.Kind, &change.BeforeHash, &change.AfterHash); err != nil {
			_ = changeRows.Close()
			return err
		}
		value.Changes = append(value.Changes, change)
	}
	if err = changeRows.Close(); err != nil {
		return err
	}
	var proofBody []byte
	var record delivery.ProofRecord
	err = store.db.QueryRowContext(ctx, `SELECT proof_body,payload_hash,proof_digest,signature FROM formal_delivery_proofs WHERE package_id=$1 AND version_no=$2`, value.PackageID, value.Number).Scan(&proofBody, &record.PayloadHash, &record.Digest, &record.Signature)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if err = json.Unmarshal(proofBody, &record.Proof); err != nil {
		return err
	}
	value.Proof = &record
	return nil
}

func nullableString(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func formalConflict(err error) error {
	var databaseError *pq.Error
	if errors.As(err, &databaseError) && databaseError.Code == "23505" {
		return delivery.ErrContentConflict
	}
	return err
}
