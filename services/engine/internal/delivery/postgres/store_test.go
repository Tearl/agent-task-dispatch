package postgres

import (
	"testing"
	"time"
)

func TestFreezeScopeBindsEveryRequiredBusinessInput(t *testing.T) {
	now := time.Date(2026, 8, 22, 12, 0, 0, 0, time.UTC)
	source := scopeSource{
		TaskSpecHash:   digest("spec"),
		AcceptanceHash: digest("acceptance"),
		AcceptanceJSON: `[{"id":"quality","title":"Quality","description":"Complete","weight":100}]`,
		OverviewID:     digest("overview"),
		OverviewHash:   digest("overview-content"),
		OverviewRef:    "artifact://overview",
		DeliveryFormat: "markdown",
		Language:       "zh-CN",
		CostCap:        "20",
		Inputs:         []string{"input://one"},
		AllowedTools:   []string{"search", "read"},
		Exclusions:     []string{"external writes"},
	}
	baseline, err := freezeScope(digest("package"), source, now)
	if err != nil {
		t.Fatal(err)
	}
	if baseline.ContentHash == "" || baseline.TaskSpecHash != source.TaskSpecHash || baseline.SelectedOverviewID != source.OverviewID || baseline.OverviewHash != source.OverviewHash || baseline.AcceptanceHash != source.AcceptanceHash || baseline.OutputConstraints["format"] != "markdown" || baseline.OutputConstraints["quantity"] != 1 || baseline.OutputConstraints["language"] != "zh-CN" || len(baseline.Inputs) != 1 || len(baseline.AllowedTools) != 2 || len(baseline.Exclusions) != 1 || baseline.ExternalCostCap != "20" {
		t.Fatalf("scope omitted a required field: %#v", baseline)
	}
	again, _ := freezeScope(digest("package"), source, now.Add(time.Hour))
	if again.ContentHash != baseline.ContentHash || again.ID != baseline.ID {
		t.Fatal("timestamps changed frozen scope identity")
	}

	mutations := []func(*scopeSource){
		func(value *scopeSource) { value.TaskSpecHash = digest("other-spec") },
		func(value *scopeSource) { value.OverviewHash = digest("other-overview") },
		func(value *scopeSource) { value.Inputs = []string{"input://two"} },
		func(value *scopeSource) { value.AcceptanceHash = digest("other-acceptance") },
		func(value *scopeSource) { value.DeliveryFormat = "json" },
		func(value *scopeSource) { value.Language = "en" },
		func(value *scopeSource) { value.AllowedTools = []string{"read"} },
		func(value *scopeSource) { value.CostCap = "19" },
		func(value *scopeSource) { value.Exclusions = []string{"network"} },
	}
	for index, mutate := range mutations {
		changed := source
		changed.Inputs = append([]string(nil), source.Inputs...)
		changed.AllowedTools = append([]string(nil), source.AllowedTools...)
		changed.Exclusions = append([]string(nil), source.Exclusions...)
		mutate(&changed)
		frozen, freezeErr := freezeScope(digest("package"), changed, now)
		if freezeErr != nil {
			t.Fatalf("mutation %d: %v", index, freezeErr)
		}
		if frozen.ContentHash == baseline.ContentHash {
			t.Fatalf("mutation %d was not bound to scope hash", index)
		}
	}
}
