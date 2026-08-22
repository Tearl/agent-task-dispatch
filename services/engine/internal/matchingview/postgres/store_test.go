package postgres

import (
	"testing"

	"github.com/example/agent-platform/engine/internal/matchingview"
	"github.com/example/agent-platform/engine/internal/overview"
)

func TestAttachOverviewsKeepsObjectiveValidationAndLatestReplacement(t *testing.T) {
	candidates := []matchingview.Candidate{{AgentID: "agent-1", Position: 1}}
	attachOverviews(candidates, []overview.Slot{
		{AgentID: "agent-1", Ordinal: 1, ID: "slot-old", Status: overview.SlotInvalid, Validation: overview.Validation{Codes: []string{"schema_invalid"}}},
		{AgentID: "agent-1", Ordinal: 4, ID: "slot-new", Status: overview.SlotValid, BillingStatus: overview.BillingCaptured, Replacement: true},
	})
	if candidates[0].Overview == nil || candidates[0].Overview.SlotID != "slot-new" || !candidates[0].Overview.Replacement || candidates[0].Overview.BillingStatus != overview.BillingCaptured {
		t.Fatalf("unexpected projection: %#v", candidates[0].Overview)
	}
}
