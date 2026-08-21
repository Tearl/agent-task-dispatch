package execution

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCallbackHandlerBindsPathAndProcessesSignedCallback(t *testing.T) {
	service, _, _, client, key, clock := executionFixture(t)
	if _, _, err := service.Create(context.Background(), validOverviewSpec()); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := service.Dispatch(context.Background(), "execution-1"); err != nil {
		t.Fatal(err)
	}
	callback := successCallback(client.createCalls[0], key, clock.Now())
	signature, err := SignCallback(callback, key)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(callback)
	handler, err := NewCallbackHandler(service)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/v1/agent-callbacks/execution-1/execution-1:attempt:1", bytes.NewReader(body))
	request.Header.Set("x-agent-signature", signature)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("signed callback status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var result CallbackResult
	if err = json.Unmarshal(recorder.Body.Bytes(), &result); err != nil || result.Outcome != CallbackAccepted || result.Execution.Status != ExecutionSucceeded {
		t.Fatalf("signed callback result=%#v err=%v", result, err)
	}
}

func TestCallbackHandlerRejectsPathSubstitutionAndUnknownFields(t *testing.T) {
	service, _, _, client, key, clock := executionFixture(t)
	if _, _, err := service.Create(context.Background(), validOverviewSpec()); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := service.Dispatch(context.Background(), "execution-1"); err != nil {
		t.Fatal(err)
	}
	callback := successCallback(client.createCalls[0], key, clock.Now())
	signature, _ := SignCallback(callback, key)
	body, _ := json.Marshal(callback)
	handler, _ := NewCallbackHandler(service)
	request := httptest.NewRequest(http.MethodPost, "/v1/agent-callbacks/other/execution-1:attempt:1", bytes.NewReader(body))
	request.Header.Set("x-agent-signature", signature)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("path substitution status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	unknownBody := append(bytes.TrimSuffix(body, []byte("}")), []byte(`,"signature":"must-not-be-persisted"}`)...)
	request = httptest.NewRequest(http.MethodPost, "/v1/agent-callbacks/execution-1/execution-1:attempt:1", bytes.NewReader(unknownBody))
	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("unknown secret-shaped field status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}
