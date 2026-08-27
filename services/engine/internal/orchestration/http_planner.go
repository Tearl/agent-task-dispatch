package orchestration

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type HTTPPlanner struct {
	endpoint, token string
	client          *http.Client
}

func NewHTTPPlanner(baseURL, token string, timeout time.Duration) (*HTTPPlanner, error) {
	parsed, err := url.Parse(strings.TrimRight(baseURL, "/"))
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || timeout <= 0 || timeout > time.Minute {
		return nil, ErrInvalidInput
	}
	return &HTTPPlanner{endpoint: strings.TrimRight(baseURL, "/") + "/v1/plans", token: token, client: &http.Client{Timeout: timeout}}, nil
}

func (planner *HTTPPlanner) Plan(ctx context.Context, task Task, agents []Agent) (Draft, error) {
	body, err := json.Marshal(map[string]any{"task": map[string]any{"id": task.ID, "specHash": task.SpecHash, "title": task.Title, "description": task.Description, "category": task.Category, "language": task.Language, "deliverables": task.Deliverables, "allowedTools": task.AllowedTools}, "agents": agents})
	if err != nil {
		return Draft{}, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, planner.endpoint, bytes.NewReader(body))
	if err != nil {
		return Draft{}, err
	}
	request.Header.Set("content-type", "application/json")
	if planner.token != "" {
		request.Header.Set("authorization", "Bearer "+planner.token)
	}
	response, err := planner.client.Do(request)
	if err != nil {
		return Draft{}, err
	}
	defer response.Body.Close()
	limited := io.LimitReader(response.Body, 262_145)
	payload, err := io.ReadAll(limited)
	if err != nil || len(payload) > 262_144 {
		return Draft{}, errors.New("orchestrator response invalid")
	}
	if response.StatusCode != http.StatusOK {
		return Draft{}, errors.New("orchestrator rejected plan")
	}
	var wire struct {
		Mode         string   `json:"mode"`
		Summary      string   `json:"summary"`
		Rationale    []string `json:"rationale"`
		Confidence   float64  `json:"confidence"`
		Steps        []Step   `json:"steps"`
		Model        string   `json:"model"`
		GraphVersion string   `json:"graphVersion"`
	}
	if json.Unmarshal(payload, &wire) != nil {
		return Draft{}, errors.New("orchestrator response invalid")
	}
	return Draft{Mode: wire.Mode, Summary: wire.Summary, Rationale: wire.Rationale, Confidence: wire.Confidence, Steps: wire.Steps, Model: wire.Model, GraphVersion: wire.GraphVersion}, nil
}
