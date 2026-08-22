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
