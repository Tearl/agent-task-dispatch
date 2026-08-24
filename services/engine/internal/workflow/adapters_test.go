package workflow

import (
	"encoding/json"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestExecutionInputUsesCategoryBoundAgentContract(t *testing.T) {
	image, err := executionInput(TaskInput{ExpertType: "图像生成", Description: "生成一张蓝色海报"})
	if err != nil || string(image) != `{"prompt":"生成一张蓝色海报","size":"1280x1280","quality":"hd"}` {
		t.Fatalf("unexpected image input: %s err=%v", image, err)
	}
	code, err := executionInput(TaskInput{ExpertType: "image-to-code", Description: "还原页面，api_key=should-not-leak", DeliveryFormat: "React", Inputs: []string{`{"image":{"data":"AA==","filename":"input.png","mediaType":"image/png"},"secret":"drop-me","prompt":"untrusted"}`}})
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if json.Unmarshal(code, &decoded) != nil || decoded["prompt"] != "还原页面，[已脱敏]" || decoded["target"] != "React" || decoded["secret"] != nil {
		t.Fatalf("unexpected image-to-code input: %s", code)
	}
	if _, err = executionInput(TaskInput{ExpertType: "image-to-code"}); err == nil {
		t.Fatal("missing immutable image input was accepted")
	}
}

func TestCapabilitiesAndForwardedInputURLAreCanonical(t *testing.T) {
	if values := parseCapabilities(`["analysis","render"]`); !reflect.DeepEqual(values, []string{"analysis", "render"}) {
		t.Fatalf("unexpected JSON capabilities: %#v", values)
	}
	request := httptest.NewRequest("GET", "http://engine.internal/v1/execution-inputs/task/hash", nil)
	request.Header.Set("x-forwarded-proto", "https")
	request.Header.Set("x-forwarded-host", "engine.example")
	if value := absoluteRequestURL(request); value != "https://engine.example/v1/execution-inputs/task/hash" {
		t.Fatalf("unexpected input URL: %s", value)
	}
}

func TestWorkflowRejectsDraftAssignedAndTerminalTaskStates(t *testing.T) {
	for _, status := range []string{"escrowed", "matching", "overview_generating", "awaiting_selection"} {
		if !workflowMayRun(status) {
			t.Fatalf("active workflow state %q was rejected", status)
		}
	}
	for _, status := range []string{"draft", "pending_escrow", "assigned", "in_progress", "submitted", "disputed", "settled", "cancelled", "refunded"} {
		if workflowMayRun(status) {
			t.Fatalf("non-workflow state %q was accepted", status)
		}
	}
}

func TestWorkflowTaskTransitionsAreExplicitAndRematchable(t *testing.T) {
	for _, transition := range [][2]string{{"escrowed", "matching"}, {"matching", "overview_generating"}, {"overview_generating", "awaiting_selection"}, {"awaiting_selection", "matching"}, {"overview_generating", "matching"}} {
		if !workflowTransitionAllowed(transition[0], transition[1]) {
			t.Fatalf("valid transition %q -> %q was rejected", transition[0], transition[1])
		}
	}
	for _, transition := range [][2]string{{"pending_escrow", "matching"}, {"escrowed", "overview_generating"}, {"matching", "awaiting_selection"}, {"assigned", "matching"}, {"settled", "matching"}} {
		if workflowTransitionAllowed(transition[0], transition[1]) {
			t.Fatalf("invalid transition %q -> %q was accepted", transition[0], transition[1])
		}
	}
}
