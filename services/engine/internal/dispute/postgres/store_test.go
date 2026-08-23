package postgres

import (
	"database/sql"
	"strings"
	"testing"

	"github.com/example/agent-platform/engine/internal/dispute"
)

func TestStoreRejectsInvalidResolverAddress(t *testing.T) {
	if _, err := NewStore(&sql.DB{}, "resolver.example"); err == nil {
		t.Fatal("invalid resolver address accepted")
	}
}

func TestSettlementEventDropsSignaturesAfterVerification(t *testing.T) {
	body := string(disputeEventBody(dispute.Command{Kind: "settlement", Input: dispute.SettlementInput{
		PublisherBPS: 5000, EvidenceRoot: "sha256:root", AgreementHash: "sha256:agreement",
		PublisherSignature: "publisher-secret-signature", AgentSignature: "agent-secret-signature", Verified: true,
	}}))
	if strings.Contains(strings.ToLower(body), "signature") || strings.Contains(body, "secret") || !strings.Contains(body, `"verified":true`) {
		t.Fatalf("unsafe settlement event body: %s", body)
	}
}
