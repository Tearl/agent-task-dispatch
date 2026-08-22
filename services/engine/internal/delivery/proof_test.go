package delivery

import "testing"

func TestProofSignerBindsEveryProofFieldDeterministically(t *testing.T) {
	signer, err := NewECDSAProofSigner("0000000000000000000000000000000000000000000000000000000000000001")
	if err != nil {
		t.Fatal(err)
	}
	proof := Proof{Version: ProofVersion, TaskID: "task", AssignmentID: "assignment", DeliveryUnit: "default", PackageID: testDigest, ScopeHash: testDigest, FormalVersion: 2, PackageAggregateVersion: 4, WorkNonce: 2, AgentID: "agent", ContentHash: testDigest, ParentContentHash: testDigest, FeedbackDigest: testDigest, AgentResponseHash: testDigest, ChangeSummaryHash: testDigest, PolicyHash: testDigest, Deadline: 100}
	payload, digest, signature, err := signer.Sign(proof)
	if err != nil || !validDigest(payload) || !validDigest(digest) || len(signature) != 132 {
		t.Fatalf("payload=%s digest=%s signature=%s err=%v", payload, digest, signature, err)
	}
	payloadAgain, digestAgain, signatureAgain, _ := signer.Sign(proof)
	if payloadAgain != payload || digestAgain != digest || signatureAgain != signature {
		t.Fatal("proof signature is not deterministic")
	}
	proof.WorkNonce++
	_, changed, _, _ := signer.Sign(proof)
	if changed == digest {
		t.Fatal("work nonce was not bound to proof")
	}
	proof.ChangeOrderID = testDigest
	_, changeOrderDigest, _, _ := signer.Sign(proof)
	if changeOrderDigest == changed {
		t.Fatal("change order was not bound to proof")
	}
}

func TestProofSignerRejectsInvalidPrivateKeys(t *testing.T) {
	for _, key := range []string{"", "01", "0000000000000000000000000000000000000000000000000000000000000000"} {
		if _, err := NewECDSAProofSigner(key); err == nil {
			t.Fatalf("invalid key accepted: %q", key)
		}
	}
}
