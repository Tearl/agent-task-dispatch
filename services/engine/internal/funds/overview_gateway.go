package funds

import (
	"context"

	"github.com/example/agent-platform/engine/internal/overview"
)

// OverviewGateway keeps overview orchestration free of ledger rules while
// adapting its stable allocation contract to the authoritative funds service.
type OverviewGateway struct{ service *Service }

func NewOverviewGateway(service *Service) (*OverviewGateway, error) {
	if service == nil {
		return nil, ErrInvalidInput
	}
	return &OverviewGateway{service: service}, nil
}

func (gateway *OverviewGateway) AuthorizeOverview(ctx context.Context, request overview.AllocationRequest) (overview.Allocation, bool, error) {
	allocation, replay, err := gateway.service.AuthorizeOverview(ctx, OverviewAuthorization{IdempotencyKey: request.IdempotencyKey, TaskID: request.TaskID, TaskSpecHash: request.TaskSpecHash, SnapshotID: request.SnapshotID, MatchRevision: request.MatchRevision, AgentID: request.AgentID, PriceVersion: request.PriceVersion, QuoteHash: request.QuoteHash, OverviewPrice: request.OverviewPrice, ExternalCostCap: request.ExternalCostCap, Deadline: request.Deadline})
	if err != nil {
		return overview.Allocation{}, false, err
	}
	return overview.Allocation{ID: allocation.ID, CostCap: allocation.CostCap, Deadline: allocation.Deadline}, replay, nil
}

func (gateway *OverviewGateway) CaptureOverview(ctx context.Context, allocationID string, claim overview.BillingClaim) (bool, error) {
	_, replay, err := gateway.service.CaptureOverview(ctx, allocationID, OverviewCapture{TaskID: claim.TaskID, TaskSpecHash: claim.TaskSpecHash, MatchRevision: claim.MatchRevision, LogicalExecutionID: claim.LogicalExecutionID, AgentID: claim.AgentID, QuoteHash: claim.QuoteHash, ContentHash: claim.ContentHash, OverviewAmount: claim.Amount, UsedCost: claim.UsedCost})
	return replay, err
}

func (gateway *OverviewGateway) ReleaseOverview(ctx context.Context, allocationID, reasonCode string) (bool, error) {
	_, replay, err := gateway.service.ReleaseOverview(ctx, allocationID, reasonCode)
	return replay, err
}

var _ overview.AllocationGateway = (*OverviewGateway)(nil)
