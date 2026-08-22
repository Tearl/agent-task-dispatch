package overview

import (
	"reflect"
	"testing"
	"time"
)

func TestValidateArtifactObjectiveRules(t *testing.T) {
	now := time.Date(2026, 8, 22, 10, 0, 0, 0, time.UTC)
	body := validResultBody("objective")
	valid := ValidateArtifact(body, digestBytes(body), now, now.Add(time.Minute), ToolEvidence{Complete: true, Tools: []string{"read"}}, []string{"read"})
	if !valid.Valid || len(valid.Codes) != 0 {
		t.Fatalf("valid artifact rejected: %#v", valid)
	}
	unsafe := ValidateArtifact(body, digestBytes([]byte("different")), now.Add(2*time.Minute), now.Add(time.Minute), ToolEvidence{Complete: false, Tools: []string{"email"}, ExternalWriteAttempts: 1}, []string{"read"})
	expected := []string{"content_hash_mismatch", "deadline_exceeded", "external_write_attempted", "tool_evidence_missing", "tool_not_allowed"}
	if unsafe.Valid || !reflect.DeepEqual(unsafe.Codes, expected) {
		t.Fatalf("objective failures changed: got=%#v want=%#v", unsafe, expected)
	}
}

func TestValidateArtifactRejectsUnknownTrailingAndIncompleteDocuments(t *testing.T) {
	now := time.Date(2026, 8, 22, 10, 0, 0, 0, time.UTC)
	for name, body := range map[string][]byte{
		"unknown field": []byte(`{"schemaVersion":"overview-result-v1","unknown":true}`),
		"trailing json": append(validResultBody("trailing"), []byte(` {}`)...),
		"incomplete":    []byte(`{"schemaVersion":"overview-result-v1","understandingSummary":"Summary"}`),
	} {
		t.Run(name, func(t *testing.T) {
			validation := ValidateArtifact(body, digestBytes(body), now, now.Add(time.Minute), ToolEvidence{Complete: true}, []string{"read"})
			if validation.Valid || len(validation.Codes) == 0 {
				t.Fatalf("invalid document accepted: %#v", validation)
			}
		})
	}
}
