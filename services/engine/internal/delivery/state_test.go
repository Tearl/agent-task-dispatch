package delivery

import (
	"errors"
	"testing"
)

func TestIncludedVersionStateMachineAllocatesV1ThroughV3Only(t *testing.T) {
	packageValue := Package{IncludedVersions: IncludedVersions, MaximumVersions: MaximumVersions, AggregateVersion: 1}
	next, aggregate, err := NextIncludedVersion(packageValue, nil, StartInput{ExpectedPackageVersion: 0, WorkNonce: 1})
	if err != nil || next != 1 || aggregate != 2 {
		t.Fatalf("V1 allocation: next=%d aggregate=%d err=%v", next, aggregate, err)
	}
	previous := &Version{Number: 1, Status: VersionReview, ContentHash: testDigest, WorkNonce: 1}
	packageValue.AllocatedVersion, packageValue.AggregateVersion = next, aggregate
	revision := &RevisionBinding{ParentVersion: 1, ParentContentHash: testDigest, FeedbackSetID: testDigest, FeedbackDigest: testDigest}
	next, aggregate, err = NextIncludedVersion(packageValue, previous, StartInput{ExpectedPackageVersion: 2, WorkNonce: 2, Revision: revision})
	if err != nil || next != 2 || aggregate != 3 {
		t.Fatalf("V2 allocation: next=%d aggregate=%d err=%v", next, aggregate, err)
	}
	previous = &Version{Number: 2, Status: VersionReview, ContentHash: testDigest, WorkNonce: 2}
	packageValue.AllocatedVersion, packageValue.AggregateVersion = next, aggregate
	revision = &RevisionBinding{ParentVersion: 2, ParentContentHash: testDigest, FeedbackSetID: testDigest, FeedbackDigest: testDigest}
	next, aggregate, err = NextIncludedVersion(packageValue, previous, StartInput{ExpectedPackageVersion: 3, WorkNonce: 3, Revision: revision})
	if err != nil || next != 3 || aggregate != 4 {
		t.Fatalf("V3 allocation: next=%d aggregate=%d err=%v", next, aggregate, err)
	}
	packageValue.AllocatedVersion, packageValue.AggregateVersion = next, aggregate
	if _, _, err = NextIncludedVersion(packageValue, &Version{Number: 3, Status: VersionReview, ContentHash: testDigest, WorkNonce: 3}, StartInput{ExpectedPackageVersion: 4, WorkNonce: 4, Revision: &RevisionBinding{ParentVersion: 3, ParentContentHash: testDigest, FeedbackSetID: testDigest, FeedbackDigest: testDigest}}); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("V4 bypassed the change-order boundary: %v", err)
	}
}

func TestIncludedVersionStateMachineRejectsStaleMissingAndActiveParents(t *testing.T) {
	packageValue := Package{IncludedVersions: IncludedVersions, MaximumVersions: MaximumVersions, AllocatedVersion: 1, AggregateVersion: 2}
	validPrevious := &Version{Number: 1, Status: VersionReview, ContentHash: testDigest, WorkNonce: 1}
	validInput := StartInput{ExpectedPackageVersion: 2, WorkNonce: 2, Revision: &RevisionBinding{ParentVersion: 1, ParentContentHash: testDigest, FeedbackSetID: testDigest, FeedbackDigest: testDigest}}
	stale := validInput
	stale.ExpectedPackageVersion = 1
	if _, _, err := NextIncludedVersion(packageValue, validPrevious, stale); !errors.Is(err, ErrStaleVersion) {
		t.Fatalf("stale aggregate accepted: %v", err)
	}
	missing := validInput
	missing.Revision = nil
	if _, _, err := NextIncludedVersion(packageValue, validPrevious, missing); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("missing feedback binding accepted: %v", err)
	}
	active := *validPrevious
	active.Status = VersionGenerating
	if _, _, err := NextIncludedVersion(packageValue, &active, validInput); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("active parent accepted: %v", err)
	}
	wrongNonce := validInput
	wrongNonce.WorkNonce = 3
	if _, _, err := NextIncludedVersion(packageValue, validPrevious, wrongNonce); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("non-sequential work nonce accepted: %v", err)
	}
}

func TestChangeOrderStateMachineAllocatesV4V5AndPermanentlyRejectsV6(t *testing.T) {
	changeOrderID := "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	packageValue := Package{IncludedVersions: IncludedVersions, MaximumVersions: MaximumVersions, AllocatedVersion: 3, AggregateVersion: 10}
	previous := &Version{Number: 3, Status: VersionReview, ContentHash: testDigest, WorkNonce: 3}
	revision := &RevisionBinding{ParentVersion: 3, ParentContentHash: testDigest, FeedbackSetID: testDigest, FeedbackDigest: testDigest, FeedbackAggregateVersion: 9}
	next, aggregate, err := NextIncludedVersion(packageValue, previous, StartInput{ExpectedPackageVersion: 10, WorkNonce: 4, Revision: revision, ChangeOrderID: changeOrderID})
	if err != nil || next != 4 || aggregate != 11 {
		t.Fatalf("V4 allocation: next=%d aggregate=%d err=%v", next, aggregate, err)
	}
	packageValue.AllocatedVersion, packageValue.AggregateVersion = 4, 20
	previous = &Version{Number: 4, Status: VersionReview, ContentHash: testDigest, WorkNonce: 4}
	revision.ParentVersion = 4
	next, aggregate, err = NextIncludedVersion(packageValue, previous, StartInput{ExpectedPackageVersion: 20, WorkNonce: 5, Revision: revision, ChangeOrderID: changeOrderID})
	if err != nil || next != 5 || aggregate != 21 {
		t.Fatalf("V5 allocation: next=%d aggregate=%d err=%v", next, aggregate, err)
	}
	packageValue.AllocatedVersion, packageValue.AggregateVersion = 5, 30
	previous = &Version{Number: 5, Status: VersionReview, ContentHash: testDigest, WorkNonce: 5}
	revision.ParentVersion = 5
	if _, _, err = NextIncludedVersion(packageValue, previous, StartInput{ExpectedPackageVersion: 30, WorkNonce: 6, Revision: revision, ChangeOrderID: changeOrderID}); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("V6 was not permanently rejected: %v", err)
	}
}
