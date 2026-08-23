package delivery

// NextIncludedVersion is the single domain gate for V1-V3 allocation. The
// repository supplies a row-locked package and previous version, then persists
// the returned version and aggregate numbers in the same transaction.
func NextIncludedVersion(packageValue Package, previous *Version, input StartInput) (int, int64, error) {
	expected := packageValue.AggregateVersion
	if packageValue.AllocatedVersion == 0 {
		expected = 0
	}
	if input.ExpectedPackageVersion != expected {
		return 0, 0, ErrStaleVersion
	}
	next := packageValue.AllocatedVersion + 1
	if next < 1 || next > packageValue.MaximumVersions || next > MaximumVersions {
		return 0, 0, ErrInvalidState
	}
	if next == 1 {
		if previous != nil || input.Revision != nil || input.WorkNonce != 1 || input.ChangeOrderID != "" {
			return 0, 0, ErrInvalidState
		}
		return next, packageValue.AggregateVersion + 1, nil
	}
	if previous == nil || input.Revision == nil || previous.Number != next-1 || input.Revision.ParentVersion != previous.Number || previous.Status != VersionReview || previous.ContentHash != input.Revision.ParentContentHash || input.WorkNonce != previous.WorkNonce+1 {
		return 0, 0, ErrInvalidState
	}
	if (next <= packageValue.IncludedVersions && input.ChangeOrderID != "") || (next > packageValue.IncludedVersions && input.ChangeOrderID == "") {
		return 0, 0, ErrInvalidState
	}
	return next, packageValue.AggregateVersion + 1, nil
}

type SettlementGateSnapshot struct {
	IntentPackageAggregate  int64
	CurrentPackageAggregate int64
	IntentFormalVersion     int
	CurrentFormalVersion    int
	VersionStatus           string
	IntentContentHash       string
	CurrentContentHash      string
	IntentProofDigest       string
	CurrentProofDigest      string
	IntentWorkNonce         uint64
	VersionWorkNonce        uint64
	CanonicalWorkNonce      uint64
	ChangeOrderReady        bool
}

// SettlementGate decides eligibility independently from transaction
// submission. A canonical receipt confirms only a proof that is still current.
func SettlementGate(value SettlementGateSnapshot) SettlementEligibility {
	reason := ""
	switch {
	case value.IntentPackageAggregate != value.CurrentPackageAggregate:
		reason = "package_advanced"
	case value.IntentFormalVersion != value.CurrentFormalVersion:
		reason = "newer_version"
	case value.VersionStatus != VersionReview:
		reason = "version_not_reviewable"
	case value.IntentContentHash != value.CurrentContentHash || value.IntentProofDigest != value.CurrentProofDigest || value.IntentWorkNonce != value.VersionWorkNonce:
		reason = "proof_mismatch"
	case value.CanonicalWorkNonce == 0:
		reason = "chain_projection_pending"
	case value.IntentWorkNonce != value.CanonicalWorkNonce:
		reason = "work_nonce_advanced"
	case value.IntentFormalVersion > IncludedVersions && !value.ChangeOrderReady:
		reason = "change_order_not_funded"
	}
	return SettlementEligibility{Eligible: reason == "", ReasonCode: reason}
}
