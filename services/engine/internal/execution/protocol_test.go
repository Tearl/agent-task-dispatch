package execution

import (
	"testing"
	"time"
)

func TestValidateSpecEnforcesStageSpecificProtocol(t *testing.T) {
	now := time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)
	overview := validOverviewSpec()
	if err := ValidateSpec(overview, now); err != nil {
		t.Fatalf("valid overview rejected: %v", err)
	}
	formal := overview
	formal.Stage = StageFormal
	formal.Overview = nil
	formal.ToolPolicy = ToolPolicy{Mode: ToolPolicyScoped, AllowedTools: []string{"read", "code"}}
	formal.Formal = &FormalBinding{AssignmentID: "assignment-1", Package: "standard", Version: 1, AggregateVersion: 4, WorkNonce: 2}
	if err := ValidateSpec(formal, now); err != nil {
		t.Fatalf("valid formal execution rejected: %v", err)
	}
	tests := []struct {
		name  string
		alter func(*Spec)
	}{
		{name: "overview write tools", alter: func(value *Spec) { value.ToolPolicy.Mode = ToolPolicyScoped }},
		{name: "mixed bindings", alter: func(value *Spec) { value.Formal = formal.Formal }},
		{name: "invalid cost", alter: func(value *Spec) { value.CostCap = "01" }},
		{name: "insecure endpoint", alter: func(value *Spec) { value.AgentEndpoint = "http://agent.example" }},
		{name: "expired", alter: func(value *Spec) { value.Deadline = now }},
		{name: "duplicate tools", alter: func(value *Spec) { value.ToolPolicy.AllowedTools = []string{"read", "read"} }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			changed := overview
			test.alter(&changed)
			if err := ValidateSpec(changed, now); err == nil {
				t.Fatal("expected invalid execution spec")
			}
		})
	}
}

func TestEveryOperationEnvelopeCarriesFencingAndRequiredBindings(t *testing.T) {
	spec := validOverviewSpec()
	execution := Execution{Spec: spec}
	attempt := Attempt{LogicalExecutionID: spec.LogicalExecutionID, AttemptID: "attempt-1", FencingToken: 7}
	for _, operation := range []string{"create", "status", "cancel", "deliverable"} {
		envelope, err := BuildEnvelope(execution, attempt, operation, "https://engine.example/callback", "once-nonce")
		if err != nil {
			t.Fatal(err)
		}
		if envelope.ProtocolVersion != ProtocolVersion || envelope.Operation != operation || envelope.FencingToken != 7 || envelope.Overview == nil || envelope.TaskSpecHash != spec.TaskSpecHash || envelope.CostCap != spec.CostCap || envelope.CallbackNonce == "" {
			t.Fatalf("operation %q lost protocol binding: %#v", operation, envelope)
		}
	}
}
