package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/example/agent-platform/engine/internal/delivery"
	"github.com/example/agent-platform/engine/internal/persistence"
	"github.com/lib/pq"
)

func (store *Store) ProposeChangeOrder(ctx context.Context, mutation delivery.Mutation, taskID string, input delivery.ProposeChangeOrderInput, draft delivery.ChangeOrder) (delivery.ChangeOrder, bool, error) {
	tx, err := store.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return delivery.ChangeOrder{}, false, err
	}
	defer func() { _ = tx.Rollback() }()
	if err = lockChange(ctx, tx, mutation, taskID); err != nil {
		return delivery.ChangeOrder{}, false, err
	}
	if replay, found, replayErr := replayChange(ctx, tx, mutation, "propose"); found || replayErr != nil {
		if replayErr == nil {
			replayErr = tx.Commit()
		}
		return replay, true, replayErr
	}
	packageValue, err := loadPackage(tx.QueryRowContext(ctx, packageSelect+` WHERE task_id=$1 AND publisher_id=$2 FOR UPDATE`, taskID, mutation.PublisherID))
	if errors.Is(err, sql.ErrNoRows) {
		return delivery.ChangeOrder{}, false, delivery.ErrNotFound
	}
	if err != nil {
		return delivery.ChangeOrder{}, false, err
	}
	if packageValue.ID != input.PackageID || packageValue.AggregateVersion != input.ExpectedPackageVersion || packageValue.AllocatedVersion != input.TriggerVersion || input.TriggerVersion < delivery.IncludedVersions || input.TriggerVersion >= delivery.MaximumVersions {
		return delivery.ChangeOrder{}, false, delivery.ErrStaleVersion
	}
	parent, err := loadVersion(tx.QueryRowContext(ctx, versionSelect+` WHERE package_id=$1 AND version_no=$2 FOR UPDATE`, packageValue.ID, input.TriggerVersion))
	if err != nil {
		return delivery.ChangeOrder{}, false, err
	}
	if parent.Status != delivery.VersionReview || parent.ContentHash != input.TriggerContentHash {
		return delivery.ChangeOrder{}, false, delivery.ErrInvalidState
	}
	var feedbackValid bool
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM formal_feedback_sets WHERE feedback_set_id=$1 AND package_id=$2 AND parent_version=$3 AND parent_content_hash=$4 AND feedback_digest=$5)`, input.FeedbackSetID, packageValue.ID, input.TriggerVersion, input.TriggerContentHash, input.FeedbackDigest).Scan(&feedbackValid); err != nil {
		return delivery.ChangeOrder{}, false, err
	}
	if !feedbackValid {
		return delivery.ChangeOrder{}, false, delivery.ErrContentConflict
	}
	var taskStatus string
	if err = tx.QueryRowContext(ctx, `SELECT status FROM tasks WHERE task_id=$1 FOR UPDATE`, taskID).Scan(&taskStatus); err != nil {
		return delivery.ChangeOrder{}, false, err
	}
	if taskStatus != "revision_requested" && taskStatus != "formal_review" {
		return delivery.ChangeOrder{}, false, delivery.ErrInvalidState
	}
	var now time.Time
	if err = tx.QueryRowContext(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
		return delivery.ChangeOrder{}, false, err
	}
	if !input.Deadline.After(now) {
		return delivery.ChangeOrder{}, false, delivery.ErrInvalidInput
	}
	draft.BaseScopeID, draft.BaseScopeHash = parent.ScopeID, parent.ScopeHash
	draft.PackageAggregateVersion, draft.CreatedAt, draft.UpdatedAt = packageValue.AggregateVersion+1, now, now
	differences, _ := json.Marshal(draft.Differences)
	_, err = tx.ExecContext(ctx, `INSERT INTO formal_change_orders (change_order_id,change_order_version,package_id,task_id,target_version,trigger_version,trigger_content_hash,feedback_set_id,feedback_digest,base_scope_id,base_scope_hash,new_spec_hash,difference_digest,scope_differences,requested_price,authorized_price,package_aggregate_version,aggregate_version,status,deadline,created_at,updated_at) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,0,$16,1,'responsibility_pending',$17,$18,$18)`, draft.ID, delivery.ChangeOrderVersion, draft.PackageID, taskID, draft.TargetVersion, draft.TriggerVersion, draft.TriggerContentHash, draft.FeedbackSetID, draft.FeedbackDigest, draft.BaseScopeID, draft.BaseScopeHash, draft.NewSpecHash, draft.DifferenceDigest, differences, draft.RequestedPrice, draft.PackageAggregateVersion, draft.Deadline, now)
	if err != nil {
		return delivery.ChangeOrder{}, false, formalConflict(err)
	}
	if _, err = tx.ExecContext(ctx, `UPDATE formal_packages SET aggregate_version=$1,updated_at=$2 WHERE package_id=$3`, draft.PackageAggregateVersion, now, packageValue.ID); err != nil {
		return delivery.ChangeOrder{}, false, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE tasks SET status='change_order_pending',aggregate_version=aggregate_version+1,updated_at=$1 WHERE task_id=$2`, now, taskID); err != nil {
		return delivery.ChangeOrder{}, false, err
	}
	if err = insertChangeEvent(ctx, tx, draft.ID, "proposed", mutation.PublisherID, map[string]any{"targetVersion": draft.TargetVersion, "differenceDigest": draft.DifferenceDigest}, now); err != nil {
		return delivery.ChangeOrder{}, false, err
	}
	if err = storeChangeRequest(ctx, tx, mutation, "propose", taskID, draft, now); err != nil {
		return delivery.ChangeOrder{}, false, err
	}
	if err = tx.Commit(); err != nil {
		return delivery.ChangeOrder{}, false, err
	}
	return draft, false, nil
}

func (store *Store) DecideChangeOrder(ctx context.Context, mutation delivery.Mutation, isAdmin bool, taskID, changeOrderID string, input delivery.DecideChangeOrderInput) (delivery.ChangeOrder, bool, error) {
	if !isAdmin {
		return delivery.ChangeOrder{}, false, delivery.ErrForbidden
	}
	tx, err := store.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return delivery.ChangeOrder{}, false, err
	}
	defer func() { _ = tx.Rollback() }()
	if err = lockChangeOrder(ctx, tx, mutation, taskID, changeOrderID); err != nil {
		return delivery.ChangeOrder{}, false, err
	}
	if replay, found, replayErr := replayChange(ctx, tx, mutation, "decide"); found || replayErr != nil {
		if replayErr == nil {
			replayErr = tx.Commit()
		}
		return replay, true, replayErr
	}
	value, err := loadChangeOrder(tx.QueryRowContext(ctx, changeOrderSelect+` WHERE change_order_id=$1 AND task_id=$2 FOR UPDATE`, changeOrderID, taskID))
	if errors.Is(err, sql.ErrNoRows) {
		return delivery.ChangeOrder{}, false, delivery.ErrNotFound
	}
	if err != nil {
		return delivery.ChangeOrder{}, false, err
	}
	if value.Status != delivery.ChangeResponsibilityPending || value.AggregateVersion != input.ExpectedVersion {
		return delivery.ChangeOrder{}, false, delivery.ErrStaleVersion
	}
	var publisherID, providerID string
	var packageAggregate int64
	if err = tx.QueryRowContext(ctx, `SELECT publisher_id,provider_id,aggregate_version FROM formal_packages WHERE package_id=$1 FOR UPDATE`, value.PackageID).Scan(&publisherID, &providerID, &packageAggregate); err != nil {
		return delivery.ChangeOrder{}, false, err
	}
	policy, err := store.responsibilityPolicy(value, input, publisherID, providerID)
	if err != nil {
		return delivery.ChangeOrder{}, false, err
	}
	if input.Responsibility == delivery.ResponsibilityPlatform {
		var ownerExists bool
		if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM users WHERE user_id=$1)`, store.platformIncidentID).Scan(&ownerExists); err != nil {
			return delivery.ChangeOrder{}, false, err
		}
		if !ownerExists {
			return delivery.ChangeOrder{}, false, delivery.ErrDependencyPending
		}
	}
	var now time.Time
	if err = tx.QueryRowContext(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
		return delivery.ChangeOrder{}, false, err
	}
	if !value.Deadline.After(now) {
		return delivery.ChangeOrder{}, false, delivery.ErrInvalidState
	}
	if policy.FundAccountID != "" {
		if _, err = tx.ExecContext(ctx, `INSERT INTO fund_accounts (account_id,account_class,account_type,task_id,reference_id,asset_key,principal_owner_id,residual_recipient_id,refund_policy_version,state,balance,created_at,updated_at) VALUES ($1,'business','change_order_escrow',$2,$3,$4,$5,$6,'change-order-responsibility-v1','open',0,$7,$7)`, policy.FundAccountID, taskID, value.ID, store.asset, policy.PrincipalOwnerID, policy.ResidualRecipientID, now); err != nil {
			return delivery.ChangeOrder{}, false, formalConflict(err)
		}
	}
	value.Responsibility, value.ResponsibilityReasonCode, value.FundingSource = input.Responsibility, input.ReasonCode, policy.FundingSource
	value.FundAccountID, value.PrincipalOwnerID, value.ResidualRecipientID = policy.FundAccountID, policy.PrincipalOwnerID, policy.ResidualRecipientID
	value.PublisherCompensationIrrevocable, value.AuthorizedPrice = input.PublisherCompensationIrrevocable, policy.AuthorizedPrice
	value.AggregateVersion, value.PackageAggregateVersion, value.Status, value.UpdatedAt = input.ExpectedVersion+1, packageAggregate+1, delivery.ChangeAwaitingAcceptance, now
	var fundAccountType any
	if value.FundAccountID != "" {
		fundAccountType = "change_order_escrow"
	}
	_, err = tx.ExecContext(ctx, `UPDATE formal_change_orders SET authorized_price=$1,responsibility=$2,responsibility_reason_code=$3,funding_source=$4,fund_account_id=$5,fund_account_type=$6,principal_owner_id=$7,residual_recipient_id=$8,publisher_compensation_irrevocable=$9,package_aggregate_version=$10,aggregate_version=$11,status='awaiting_acceptance',updated_at=$12 WHERE change_order_id=$13`, value.AuthorizedPrice, value.Responsibility, value.ResponsibilityReasonCode, value.FundingSource, nullableString(value.FundAccountID), fundAccountType, value.PrincipalOwnerID, value.ResidualRecipientID, value.PublisherCompensationIrrevocable, value.PackageAggregateVersion, value.AggregateVersion, now, value.ID)
	if err != nil {
		return delivery.ChangeOrder{}, false, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE formal_packages SET aggregate_version=$1,updated_at=$2 WHERE package_id=$3`, value.PackageAggregateVersion, now, value.PackageID); err != nil {
		return delivery.ChangeOrder{}, false, err
	}
	if err = insertChangeEvent(ctx, tx, value.ID, "responsibility_decided", mutation.PublisherID, map[string]any{"responsibility": value.Responsibility, "fundingSource": value.FundingSource, "authorizedPrice": value.AuthorizedPrice}, now); err != nil {
		return delivery.ChangeOrder{}, false, err
	}
	if err = storeChangeRequest(ctx, tx, mutation, "decide", taskID, value, now); err != nil {
		return delivery.ChangeOrder{}, false, err
	}
	if err = tx.Commit(); err != nil {
		return delivery.ChangeOrder{}, false, err
	}
	return value, false, nil
}

func (store *Store) AcceptChangeOrder(ctx context.Context, mutation delivery.Mutation, taskID, changeOrderID string, input delivery.ChangeOrderVersionInput) (delivery.ChangeOrder, bool, error) {
	return store.transitionChangeOrder(ctx, mutation, false, taskID, changeOrderID, "accept", input)
}

func (store *Store) ActivateChangeOrder(ctx context.Context, mutation delivery.Mutation, isAdmin bool, taskID, changeOrderID string, input delivery.ChangeOrderVersionInput) (delivery.ChangeOrder, bool, error) {
	return store.transitionChangeOrder(ctx, mutation, isAdmin, taskID, changeOrderID, "activate", input)
}

func (store *Store) transitionChangeOrder(ctx context.Context, mutation delivery.Mutation, isAdmin bool, taskID, changeOrderID, operation string, input delivery.ChangeOrderVersionInput) (delivery.ChangeOrder, bool, error) {
	tx, err := store.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return delivery.ChangeOrder{}, false, err
	}
	defer func() { _ = tx.Rollback() }()
	if err = lockChangeOrder(ctx, tx, mutation, taskID, changeOrderID); err != nil {
		return delivery.ChangeOrder{}, false, err
	}
	if replay, found, replayErr := replayChange(ctx, tx, mutation, operation); found || replayErr != nil {
		if replayErr == nil {
			replayErr = tx.Commit()
		}
		return replay, true, replayErr
	}
	value, err := loadChangeOrder(tx.QueryRowContext(ctx, changeOrderSelect+` WHERE change_order_id=$1 AND task_id=$2 FOR UPDATE`, changeOrderID, taskID))
	if errors.Is(err, sql.ErrNoRows) {
		return delivery.ChangeOrder{}, false, delivery.ErrNotFound
	}
	if err != nil {
		return delivery.ChangeOrder{}, false, err
	}
	var publisherID string
	var packageAggregate int64
	if err = tx.QueryRowContext(ctx, `SELECT publisher_id,aggregate_version FROM formal_packages WHERE package_id=$1 FOR UPDATE`, value.PackageID).Scan(&publisherID, &packageAggregate); err != nil {
		return delivery.ChangeOrder{}, false, err
	}
	if !isAdmin && publisherID != mutation.PublisherID {
		return delivery.ChangeOrder{}, false, delivery.ErrNotFound
	}
	if value.AggregateVersion != input.ExpectedVersion {
		return delivery.ChangeOrder{}, false, delivery.ErrStaleVersion
	}
	var now time.Time
	if err = tx.QueryRowContext(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
		return delivery.ChangeOrder{}, false, err
	}
	if !value.Deadline.After(now) {
		return delivery.ChangeOrder{}, false, delivery.ErrInvalidState
	}
	if operation == "accept" {
		if value.Status != delivery.ChangeAwaitingAcceptance {
			return delivery.ChangeOrder{}, false, delivery.ErrInvalidState
		}
		value.Status = delivery.ChangeReady
		if value.FundAccountID != "" {
			value.Status = delivery.ChangeAwaitingFunding
		}
		value.AcceptedAt = &now
		value.AggregateVersion++
		value.PackageAggregateVersion = packageAggregate + 1
		value.UpdatedAt = now
		_, err = tx.ExecContext(ctx, `UPDATE formal_change_orders SET status=$1,accepted_at=$2,aggregate_version=$3,package_aggregate_version=$4,updated_at=$2 WHERE change_order_id=$5`, value.Status, now, value.AggregateVersion, value.PackageAggregateVersion, value.ID)
		if err != nil {
			return delivery.ChangeOrder{}, false, err
		}
		if err = insertChangeEvent(ctx, tx, value.ID, "accepted", mutation.PublisherID, map[string]any{"status": value.Status}, now); err != nil {
			return delivery.ChangeOrder{}, false, err
		}
	} else {
		if value.Status != delivery.ChangeReady && value.Status != delivery.ChangeAwaitingFunding {
			return delivery.ChangeOrder{}, false, delivery.ErrInvalidState
		}
		if value.FundAccountID != "" {
			var enough bool
			if err = tx.QueryRowContext(ctx, `SELECT balance >= $2::numeric FROM fund_accounts WHERE account_id=$1 AND state='open' FOR UPDATE`, value.FundAccountID, value.AuthorizedPrice).Scan(&enough); errors.Is(err, sql.ErrNoRows) || !enough {
				return delivery.ChangeOrder{}, false, delivery.ErrDependencyPending
			}
			if err != nil {
				return delivery.ChangeOrder{}, false, err
			}
			if _, err = tx.ExecContext(ctx, `UPDATE fund_accounts SET state='frozen',updated_at=$1 WHERE account_id=$2 AND state='open'`, now, value.FundAccountID); err != nil {
				return delivery.ChangeOrder{}, false, err
			}
		}
		var allocated int
		if err = tx.QueryRowContext(ctx, `SELECT allocated_version FROM formal_packages WHERE package_id=$1`, value.PackageID).Scan(&allocated); err != nil {
			return delivery.ChangeOrder{}, false, err
		}
		if allocated+1 != value.TargetVersion {
			return delivery.ChangeOrder{}, false, delivery.ErrStaleVersion
		}
		scope, scopeErr := activateScope(ctx, tx, value, now)
		if scopeErr != nil {
			return delivery.ChangeOrder{}, false, scopeErr
		}
		value.NewScopeID, value.NewScopeHash, value.NewScopeRevision = scope.ID, scope.ContentHash, scope.Revision
		value.Status, value.EffectiveAt, value.UpdatedAt = delivery.ChangeEffective, &now, now
		value.AggregateVersion++
		value.PackageAggregateVersion = packageAggregate + 1
		_, err = tx.ExecContext(ctx, `UPDATE formal_change_orders SET status='effective',new_scope_id=$1,new_scope_hash=$2,new_scope_revision=$3,effective_at=$4,aggregate_version=$5,package_aggregate_version=$6,updated_at=$4 WHERE change_order_id=$7`, value.NewScopeID, value.NewScopeHash, value.NewScopeRevision, now, value.AggregateVersion, value.PackageAggregateVersion, value.ID)
		if err != nil {
			return delivery.ChangeOrder{}, false, err
		}
		updatedTask, updateErr := tx.ExecContext(ctx, `UPDATE tasks SET status='revision_requested',aggregate_version=aggregate_version+1,updated_at=$1 WHERE task_id=$2 AND status='change_order_pending'`, now, taskID)
		if updateErr != nil {
			return delivery.ChangeOrder{}, false, updateErr
		}
		if err = requireOne(updatedTask); err != nil {
			return delivery.ChangeOrder{}, false, err
		}
		if err = insertChangeEvent(ctx, tx, value.ID, "effective", mutation.PublisherID, map[string]any{"scopeId": value.NewScopeID, "scopeHash": value.NewScopeHash}, now); err != nil {
			return delivery.ChangeOrder{}, false, err
		}
	}
	if _, err = tx.ExecContext(ctx, `UPDATE formal_packages SET aggregate_version=$1,updated_at=$2 WHERE package_id=$3`, value.PackageAggregateVersion, now, value.PackageID); err != nil {
		return delivery.ChangeOrder{}, false, err
	}
	if err = storeChangeRequest(ctx, tx, mutation, operation, taskID, value, now); err != nil {
		return delivery.ChangeOrder{}, false, err
	}
	if err = tx.Commit(); err != nil {
		return delivery.ChangeOrder{}, false, err
	}
	return value, false, nil
}

type responsibilityPolicy struct{ FundingSource, FundAccountID, PrincipalOwnerID, ResidualRecipientID, AuthorizedPrice string }

func (store *Store) responsibilityPolicy(value delivery.ChangeOrder, input delivery.DecideChangeOrderInput, publisherID, providerID string) (responsibilityPolicy, error) {
	switch input.Responsibility {
	case delivery.ResponsibilityPublisher:
		if input.PublisherCompensationIrrevocable || store.asset == "" {
			return responsibilityPolicy{}, delivery.ErrInvalidInput
		}
		return responsibilityPolicy{delivery.FundingPublisher, digest("change-order-account:" + value.ID), publisherID, publisherID, value.RequestedPrice}, nil
	case delivery.ResponsibilityAgent:
		if input.PublisherCompensationIrrevocable {
			return responsibilityPolicy{}, delivery.ErrInvalidInput
		}
		return responsibilityPolicy{delivery.FundingAgentAbsorbed, "", providerID, providerID, "0"}, nil
	case delivery.ResponsibilityPlatform:
		if store.asset == "" || store.platformIncidentID == "" {
			return responsibilityPolicy{}, delivery.ErrDependencyPending
		}
		residual := store.platformIncidentID
		if input.PublisherCompensationIrrevocable {
			residual = publisherID
		}
		return responsibilityPolicy{delivery.FundingPlatformIncident, digest("change-order-account:" + value.ID), store.platformIncidentID, residual, value.RequestedPrice}, nil
	default:
		return responsibilityPolicy{}, delivery.ErrInvalidInput
	}
}

func activateScope(ctx context.Context, tx *sql.Tx, change delivery.ChangeOrder, now time.Time) (delivery.Scope, error) {
	base, err := loadScope(tx.QueryRowContext(ctx, scopeSelect+` WHERE scope_id=$1`, change.BaseScopeID))
	if err != nil {
		return delivery.Scope{}, err
	}
	revision := base.Revision + 1
	contentHash := digest(fmt.Sprintf("formal-change-scope:%s:%s:%s:%d", base.ContentHash, change.NewSpecHash, change.DifferenceDigest, revision))
	scope := base
	scope.ID, scope.Revision, scope.ContentHash, scope.TaskSpecHash, scope.ChangeOrderID, scope.Differences, scope.CreatedAt = digest("formal-scope:"+change.PackageID+":"+contentHash), revision, contentHash, change.NewSpecHash, change.ID, change.Differences, now
	criteria, _ := json.Marshal(scope.AcceptanceCriteria)
	output, _ := json.Marshal(scope.OutputConstraints)
	differences, _ := json.Marshal(scope.Differences)
	body, _ := json.Marshal(map[string]any{"baseScopeHash": base.ContentHash, "taskSpecHash": scope.TaskSpecHash, "scopeDifferences": scope.Differences, "differenceDigest": change.DifferenceDigest, "changeOrderId": change.ID})
	_, err = tx.ExecContext(ctx, `INSERT INTO formal_scope_snapshots (scope_id,package_id,scope_revision,scope_version,content_hash,task_spec_hash,selected_overview_id,overview_content_hash,overview_ref,input_snapshot,acceptance_hash,acceptance_criteria,output_constraints,allowed_tools,external_cost_cap,exclusions,scope_body,change_order_id,scope_differences,created_at) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20)`, scope.ID, scope.PackageID, scope.Revision, delivery.ScopeVersion, scope.ContentHash, scope.TaskSpecHash, scope.SelectedOverviewID, scope.OverviewHash, scope.OverviewRef, pq.Array(scope.Inputs), scope.AcceptanceHash, criteria, output, pq.Array(scope.AllowedTools), scope.ExternalCostCap, pq.Array(scope.Exclusions), body, scope.ChangeOrderID, differences, now)
	return scope, err
}

func authorizeChangeOrder(ctx context.Context, tx *sql.Tx, publisherID, taskID string, packageValue delivery.Package, next int, input delivery.StartInput) (delivery.Scope, time.Time, string, error) {
	var scopeID string
	var deadline time.Time
	var responsibility string
	err := tx.QueryRowContext(ctx, `SELECT change_order.new_scope_id,change_order.deadline,change_order.responsibility FROM formal_change_orders change_order WHERE change_order.change_order_id=$1 AND change_order.package_id=$2 AND change_order.task_id=$3 AND change_order.target_version=$4 AND change_order.status='effective' AND change_order.package_aggregate_version=$5 AND change_order.feedback_set_id=$6 AND change_order.feedback_digest=$7 AND change_order.deadline>clock_timestamp() AND EXISTS (SELECT 1 FROM formal_packages WHERE package_id=$2 AND publisher_id=$8) FOR UPDATE`, input.ChangeOrderID, packageValue.ID, taskID, next, packageValue.AggregateVersion, input.Revision.FeedbackSetID, input.Revision.FeedbackDigest, publisherID).Scan(&scopeID, &deadline, &responsibility)
	if errors.Is(err, sql.ErrNoRows) {
		return delivery.Scope{}, time.Time{}, "", delivery.ErrStaleVersion
	}
	if err != nil {
		return delivery.Scope{}, time.Time{}, "", err
	}
	scope, err := loadScope(tx.QueryRowContext(ctx, scopeSelect+` WHERE scope_id=$1`, scopeID))
	return scope, deadline, responsibility, err
}

func (store *Store) loadChangeOrders(ctx context.Context, packageID string) ([]delivery.ChangeOrder, error) {
	rows, err := store.db.QueryContext(ctx, changeOrderSelect+` WHERE package_id=$1 ORDER BY target_version`, packageID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	values := make([]delivery.ChangeOrder, 0)
	for rows.Next() {
		value, scanErr := loadChangeOrder(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		values = append(values, value)
	}
	return values, rows.Err()
}

const changeOrderSelect = `SELECT change_order_id,package_id,task_id,target_version,trigger_version,trigger_content_hash,feedback_set_id,feedback_digest,base_scope_id,base_scope_hash,COALESCE(new_scope_id,''),COALESCE(new_scope_hash,''),COALESCE(new_scope_revision,0),new_spec_hash,difference_digest,scope_differences,requested_price::text,authorized_price::text,COALESCE(responsibility,''),COALESCE(responsibility_reason_code,''),COALESCE(funding_source,''),COALESCE(fund_account_id,''),COALESCE(principal_owner_id,''),COALESCE(residual_recipient_id,''),publisher_compensation_irrevocable,package_aggregate_version,aggregate_version,status,deadline,accepted_at,effective_at,consumed_at,created_at,updated_at FROM formal_change_orders`

func loadChangeOrder(row scanner) (delivery.ChangeOrder, error) {
	var value delivery.ChangeOrder
	var differences []byte
	var accepted, effective, consumed sql.NullTime
	err := row.Scan(&value.ID, &value.PackageID, &value.TaskID, &value.TargetVersion, &value.TriggerVersion, &value.TriggerContentHash, &value.FeedbackSetID, &value.FeedbackDigest, &value.BaseScopeID, &value.BaseScopeHash, &value.NewScopeID, &value.NewScopeHash, &value.NewScopeRevision, &value.NewSpecHash, &value.DifferenceDigest, &differences, &value.RequestedPrice, &value.AuthorizedPrice, &value.Responsibility, &value.ResponsibilityReasonCode, &value.FundingSource, &value.FundAccountID, &value.PrincipalOwnerID, &value.ResidualRecipientID, &value.PublisherCompensationIrrevocable, &value.PackageAggregateVersion, &value.AggregateVersion, &value.Status, &value.Deadline, &accepted, &effective, &consumed, &value.CreatedAt, &value.UpdatedAt)
	if err == nil {
		err = json.Unmarshal(differences, &value.Differences)
	}
	if accepted.Valid {
		value.AcceptedAt = &accepted.Time
	}
	if effective.Valid {
		value.EffectiveAt = &effective.Time
	}
	if consumed.Valid {
		value.ConsumedAt = &consumed.Time
	}
	return value, err
}

func lockChange(ctx context.Context, tx *sql.Tx, mutation delivery.Mutation, taskID string) error {
	_, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0)),pg_advisory_xact_lock(hashtextextended($2,0))`, "formal-change:"+mutation.PublisherID+":"+mutation.IdempotencyKey, "formal-task:"+taskID)
	return err
}
func lockChangeOrder(ctx context.Context, tx *sql.Tx, mutation delivery.Mutation, taskID, changeOrderID string) error {
	if err := lockChange(ctx, tx, mutation, taskID); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "formal-change-order:"+changeOrderID)
	return err
}

func replayChange(ctx context.Context, tx *sql.Tx, mutation delivery.Mutation, operation string) (delivery.ChangeOrder, bool, error) {
	var requestHash, storedOperation string
	var body []byte
	err := tx.QueryRowContext(ctx, `SELECT request_hash,operation,response_body FROM formal_change_order_requests WHERE actor_id=$1 AND idempotency_key=$2`, mutation.PublisherID, mutation.IdempotencyKey).Scan(&requestHash, &storedOperation, &body)
	if errors.Is(err, sql.ErrNoRows) {
		return delivery.ChangeOrder{}, false, nil
	}
	if err != nil {
		return delivery.ChangeOrder{}, false, err
	}
	if requestHash != mutation.RequestHash || storedOperation != operation {
		return delivery.ChangeOrder{}, false, persistence.ErrIdempotencyConflict
	}
	var value delivery.ChangeOrder
	err = json.Unmarshal(body, &value)
	return value, true, err
}

func storeChangeRequest(ctx context.Context, tx *sql.Tx, mutation delivery.Mutation, operation, taskID string, value delivery.ChangeOrder, now time.Time) error {
	body, err := json.Marshal(value)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO formal_change_order_requests (actor_id,idempotency_key,request_hash,operation,task_id,change_order_id,response_body,created_at) VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`, mutation.PublisherID, mutation.IdempotencyKey, mutation.RequestHash, operation, taskID, value.ID, body, now)
	return err
}

func insertChangeEvent(ctx context.Context, tx *sql.Tx, changeOrderID, eventType, actorID string, payload any, now time.Time) error {
	var sequence int64
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(max(event_sequence),0)+1 FROM formal_change_order_events WHERE change_order_id=$1`, changeOrderID).Scan(&sequence); err != nil {
		return err
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO formal_change_order_events (event_id,change_order_id,event_sequence,event_type,actor_id,payload,occurred_at) VALUES ($1,$2,$3,$4,$5,$6,$7)`, digest(fmt.Sprintf("formal-change-event:%s:%d:%s", changeOrderID, sequence, eventType)), changeOrderID, sequence, eventType, actorID, body, now)
	return err
}
