package execution

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
)

const maxProtocolResponseBytes = 64 * 1024

type RequestAuthorizer interface {
	Authorize(context.Context, *http.Request, []byte) error
}

type HTTPClient struct {
	client     *http.Client
	authorizer RequestAuthorizer
}

func NewHTTPClient(client *http.Client, authorizer RequestAuthorizer) (*HTTPClient, error) {
	if client == nil || authorizer == nil {
		return nil, ErrInvalidInput
	}
	copyClient := *client
	copyClient.CheckRedirect = func(*http.Request, []*http.Request) error {
		return errors.New("Agent protocol redirects are not allowed")
	}
	return &HTTPClient{client: &copyClient, authorizer: authorizer}, nil
}

func (client *HTTPClient) Create(ctx context.Context, endpoint string, envelope Envelope) (CreateResponse, error) {
	var result CreateResponse
	err := client.call(ctx, endpoint, "/v1/executions", envelope, &result)
	return result, err
}

func (client *HTTPClient) Status(ctx context.Context, endpoint string, envelope Envelope) (StatusResponse, error) {
	var result StatusResponse
	err := client.call(ctx, endpoint, "/v1/executions/status", envelope, &result)
	return result, err
}

func (client *HTTPClient) Cancel(ctx context.Context, endpoint string, envelope Envelope) (CancelResponse, error) {
	var result CancelResponse
	err := client.call(ctx, endpoint, "/v1/executions/cancel", envelope, &result)
	return result, err
}

func (client *HTTPClient) Deliverable(ctx context.Context, endpoint string, envelope Envelope) (DeliverableResponse, error) {
	var result DeliverableResponse
	err := client.call(ctx, endpoint, "/v1/executions/deliverable", envelope, &result)
	return result, err
}

func (client *HTTPClient) call(ctx context.Context, endpoint, path string, envelope Envelope, result any) error {
	base, err := url.Parse(endpoint)
	if err != nil || base.Scheme != "https" || base.Host == "" || base.User != nil || base.RawQuery != "" || base.Fragment != "" || base.Path != "" && base.Path != "/" {
		return ErrInvalidInput
	}
	target, err := url.JoinPath(endpoint, path)
	if err != nil {
		return ErrInvalidInput
	}
	body, err := json.Marshal(envelope)
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(body))
	if err != nil {
		return err
	}
	request.Header.Set("accept", "application/json")
	request.Header.Set("content-type", "application/json")
	request.Header.Set("idempotency-key", envelope.IdempotencyKey)
	request.Header.Set("x-agent-protocol-version", ProtocolVersion)
	if err = client.authorizer.Authorize(ctx, request, body); err != nil {
		return errors.New("Agent request authorization failed")
	}
	response, err := client.client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("Agent protocol returned status %d", response.StatusCode)
	}
	encoded, err := io.ReadAll(io.LimitReader(response.Body, maxProtocolResponseBytes+1))
	if err != nil {
		return err
	}
	if len(encoded) > maxProtocolResponseBytes {
		return errors.New("Agent protocol response is too large")
	}
	if err = json.Unmarshal(encoded, result); err != nil {
		return errors.New("Agent protocol returned invalid JSON")
	}
	return nil
}
