package postgres

import (
	"errors"
	"testing"

	"github.com/example/agent-platform/engine/internal/delivery"
)

func TestResponsibilityPolicyKeepsEveryFundingBoundary(t *testing.T) {
	store := &Store{asset: "evm:1/native", platformIncidentID: "incident"}
	change := delivery.ChangeOrder{ID: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", RequestedPrice: "100"}
	publisher, err := store.responsibilityPolicy(change, delivery.DecideChangeOrderInput{Responsibility: delivery.ResponsibilityPublisher}, "publisher", "provider")
	if err != nil || publisher.FundingSource != delivery.FundingPublisher || publisher.PrincipalOwnerID != "publisher" || publisher.ResidualRecipientID != "publisher" || publisher.AuthorizedPrice != "100" || publisher.FundAccountID == "" {
		t.Fatalf("publisher policy=%#v err=%v", publisher, err)
	}
	agent, err := store.responsibilityPolicy(change, delivery.DecideChangeOrderInput{Responsibility: delivery.ResponsibilityAgent}, "publisher", "provider")
	if err != nil || agent.FundingSource != delivery.FundingAgentAbsorbed || agent.AuthorizedPrice != "0" || agent.FundAccountID != "" || agent.PrincipalOwnerID != "provider" {
		t.Fatalf("agent policy=%#v err=%v", agent, err)
	}
	platform, err := store.responsibilityPolicy(change, delivery.DecideChangeOrderInput{Responsibility: delivery.ResponsibilityPlatform}, "publisher", "provider")
	if err != nil || platform.FundingSource != delivery.FundingPlatformIncident || platform.PrincipalOwnerID != "incident" || platform.ResidualRecipientID != "incident" {
		t.Fatalf("platform policy=%#v err=%v", platform, err)
	}
	compensation, err := store.responsibilityPolicy(change, delivery.DecideChangeOrderInput{Responsibility: delivery.ResponsibilityPlatform, PublisherCompensationIrrevocable: true}, "publisher", "provider")
	if err != nil || compensation.ResidualRecipientID != "publisher" {
		t.Fatalf("compensation policy=%#v err=%v", compensation, err)
	}
}

func TestResponsibilityPolicyFailsClosedWithoutPlatformIncidentAccount(t *testing.T) {
	store := &Store{asset: "evm:1/native"}
	_, err := store.responsibilityPolicy(delivery.ChangeOrder{RequestedPrice: "1"}, delivery.DecideChangeOrderInput{Responsibility: delivery.ResponsibilityPlatform}, "publisher", "provider")
	if !errors.Is(err, delivery.ErrDependencyPending) {
		t.Fatalf("missing incident account accepted: %v", err)
	}
}
