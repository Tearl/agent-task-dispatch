package overview

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"slices"
	"strings"
	"time"
)

const MaxArtifactBytes int64 = 64 * 1024

type ResultDocument struct {
	SchemaVersion            string   `json:"schemaVersion"`
	UnderstandingSummary     string   `json:"understandingSummary"`
	Approach                 []string `json:"approach"`
	DeliverableStructure     []string `json:"deliverableStructure"`
	KeyRisks                 []string `json:"keyRisks"`
	EstimatedDurationSeconds int64    `json:"estimatedDurationSeconds"`
	Sample                   string   `json:"sample,omitempty"`
}

func ValidateArtifact(body []byte, contentHash string, completedAt, deadline time.Time, evidence ToolEvidence, allowedTools []string) Validation {
	codes := make([]string, 0, 8)
	if int64(len(body)) > MaxArtifactBytes {
		codes = append(codes, "artifact_too_large")
	}
	digest := sha256.Sum256(body)
	if contentHash != "sha256:"+hex.EncodeToString(digest[:]) {
		codes = append(codes, "content_hash_mismatch")
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	var document ResultDocument
	firstErr := decoder.Decode(&document)
	secondErr := decoder.Decode(&struct{}{})
	if firstErr != nil || !errors.Is(secondErr, io.EOF) {
		codes = append(codes, "format_invalid")
	} else {
		if document.SchemaVersion != ResultSchemaVersion {
			codes = append(codes, "schema_version_invalid")
		}
		if blankOrLong(document.UnderstandingSummary, 4_000) {
			codes = append(codes, "understanding_summary_invalid")
		}
		if invalidList(document.Approach, 1, 10, 1_000) {
			codes = append(codes, "approach_invalid")
		}
		if invalidList(document.DeliverableStructure, 1, 20, 1_000) {
			codes = append(codes, "deliverable_structure_invalid")
		}
		if invalidList(document.KeyRisks, 1, 10, 1_000) {
			codes = append(codes, "key_risks_invalid")
		}
		if document.EstimatedDurationSeconds < 1 || document.EstimatedDurationSeconds > int64((365*24*time.Hour)/time.Second) {
			codes = append(codes, "estimated_duration_invalid")
		}
		if len(document.Sample) > 4_000 {
			codes = append(codes, "sample_too_large")
		}
	}
	if completedAt.After(deadline) {
		codes = append(codes, "deadline_exceeded")
	}
	if !evidence.Complete {
		codes = append(codes, "tool_evidence_missing")
	}
	if evidence.ExternalWriteAttempts > 0 {
		codes = append(codes, "external_write_attempted")
	}
	allowed := make(map[string]struct{}, len(allowedTools))
	for _, tool := range allowedTools {
		allowed[tool] = struct{}{}
	}
	for _, tool := range evidence.Tools {
		if _, ok := allowed[tool]; !ok {
			codes = append(codes, "tool_not_allowed")
			break
		}
	}
	slices.Sort(codes)
	codes = slices.Compact(codes)
	return Validation{Valid: len(codes) == 0, Codes: codes}
}

func blankOrLong(value string, maximum int) bool {
	return strings.TrimSpace(value) == "" || len(value) > maximum
}

func invalidList(values []string, minimum, maximum, itemMaximum int) bool {
	if len(values) < minimum || len(values) > maximum {
		return true
	}
	for _, value := range values {
		if blankOrLong(value, itemMaximum) {
			return true
		}
	}
	return false
}
