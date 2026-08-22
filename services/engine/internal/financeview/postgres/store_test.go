package postgres

import (
	"testing"

	"github.com/example/agent-platform/engine/internal/financeview"
)

func TestPresentationStatesKeepSubmissionConfirmationRefundAndTerminalSeparate(t *testing.T) {
	cases := []struct{ reservation, tx, submission, confirmation string }{
		{"reserved", "", financeview.SubmissionNotSubmitted, financeview.ConfirmationNotObserved},
		{"submitted", "0x01", financeview.SubmissionSubmitted, financeview.ConfirmationPending},
		{"confirmed", "0x01", financeview.SubmissionSubmitted, financeview.ConfirmationConfirmed},
		{"failed", "0x01", financeview.SubmissionSubmitted, financeview.ConfirmationFailed},
		{"orphaned", "0x01", financeview.SubmissionSubmitted, financeview.ConfirmationOrphaned},
	}
	for _, test := range cases {
		state := chainState(test.reservation, test.tx)
		if state.Submission != test.submission || state.Confirmation != test.confirmation {
			t.Fatalf("%s: %#v", test.reservation, state)
		}
	}
	if refundState("escrowed") != financeview.RefundAvailable || refundState("refund_pending") != financeview.RefundPending || refundState("refunded") != financeview.RefundConfirmed || refundState("assigned") != financeview.RefundUnavailable {
		t.Fatal("refund state collapsed")
	}
	if !terminal("settled") || !terminal("refunded") || terminal("refund_pending") {
		t.Fatal("terminal state collapsed")
	}
	if state := lifecycleChainState("refund_pending", chainState("confirmed", "0x01")); state.Submission != financeview.SubmissionSubmitted || state.Confirmation != financeview.ConfirmationPending {
		t.Fatalf("refund pending collapsed into selection confirmation: %#v", state)
	}
	if state := lifecycleChainState("refunded", chainState("", "")); state.Confirmation != financeview.ConfirmationConfirmed {
		t.Fatalf("refund terminal not confirmed: %#v", state)
	}
}
