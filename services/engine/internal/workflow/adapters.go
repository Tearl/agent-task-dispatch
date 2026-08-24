package workflow

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/example/agent-platform/engine/internal/execution"
	"github.com/example/agent-platform/engine/internal/overview"
)

type TargetRecord struct {
	AgentID         string
	ProviderID      string
	Endpoint        string
	PriceVersion    int
	OverviewPrice   string
	ExternalCostCap string
}

type BriefProvider struct {
	store   *Store
	baseURL string
}

func NewBriefProvider(store *Store, baseURL string) (*BriefProvider, error) {
	parsed, err := url.Parse(strings.TrimRight(baseURL, "/"))
	if store == nil || err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, ErrInvalidInput
	}
	return &BriefProvider{store: store, baseURL: strings.TrimRight(baseURL, "/")}, nil
}

func (provider *BriefProvider) PrepareOverviewBrief(ctx context.Context, taskID, taskSpecHash string, deadline time.Time) (overview.BriefHandle, error) {
	var publisherID string
	if err := provider.store.db.QueryRowContext(ctx, `SELECT publisher_id FROM tasks WHERE task_id=$1`, taskID).Scan(&publisherID); err != nil {
		return overview.BriefHandle{}, err
	}
	task, err := provider.store.Task(ctx, publisherID, taskID)
	if err != nil || task.SpecHash != taskSpecHash {
		return overview.BriefHandle{}, ErrInvalidInput
	}
	body, err := executionInput(task)
	if err != nil {
		return overview.BriefHandle{}, err
	}
	digest := sha256.Sum256(body)
	return overview.BriefHandle{
		Ref:       fmt.Sprintf("%s/v1/execution-inputs/%s/%s", provider.baseURL, url.PathEscape(task.ID), strings.TrimPrefix(task.SpecHash, "sha256:")),
		Hash:      "sha256:" + hex.EncodeToString(digest[:]),
		ExpiresAt: deadline,
	}, nil
}

type TargetResolver struct{ Store *Store }

func (resolver TargetResolver) ResolveOverviewTarget(ctx context.Context, agentID string, priceVersion int) (overview.DispatchTarget, error) {
	target, err := resolver.Store.ResolveTarget(ctx, agentID, priceVersion)
	if err != nil {
		return overview.DispatchTarget{}, err
	}
	body, _ := json.Marshal(target)
	digest := sha256.Sum256(body)
	return overview.DispatchTarget{AgentID: target.AgentID, ProviderID: target.ProviderID, Endpoint: target.Endpoint, PriceVersion: target.PriceVersion, OverviewPrice: target.OverviewPrice, ExternalCostCap: target.ExternalCostCap, QuoteHash: "sha256:" + hex.EncodeToString(digest[:])}, nil
}

type ArtifactReader struct {
	store       *Store
	credentials *execution.RuntimeCredentialProvider
	client      *http.Client
}

func NewArtifactReader(store *Store, credentials *execution.RuntimeCredentialProvider, timeout time.Duration) (*ArtifactReader, error) {
	if store == nil || credentials == nil || timeout <= 0 || timeout > time.Minute {
		return nil, ErrInvalidInput
	}
	return &ArtifactReader{store: store, credentials: credentials, client: &http.Client{Timeout: timeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}, nil
}

func (reader *ArtifactReader) Read(ctx context.Context, ref, expectedHash string, maximum int64) ([]byte, error) {
	agentID, endpoint, err := reader.store.ArtifactAgent(ctx, ref)
	if err != nil {
		return nil, err
	}
	parsed, err := url.Parse(ref)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || !strings.HasPrefix(parsed.Path, "/v1/artifacts/") {
		return nil, ErrInvalidInput
	}
	target, err := url.Parse(endpoint)
	if err != nil || !strings.EqualFold(parsed.Scheme, target.Scheme) || !strings.EqualFold(parsed.Host, target.Host) {
		return nil, ErrInvalidInput
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, ref, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("accept", "application/json")
	if err = reader.credentials.AuthorizeAgent(request, agentID); err != nil {
		return nil, err
	}
	response, err := reader.client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK || !strings.Contains(strings.ToLower(response.Header.Get("content-type")), "application/json") || response.ContentLength > maximum {
		return nil, ErrDependencyPending
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maximum+1))
	if err != nil || int64(len(body)) > maximum {
		return nil, ErrDependencyPending
	}
	digest := sha256.Sum256(body)
	if "sha256:"+hex.EncodeToString(digest[:]) != expectedHash {
		return nil, ErrInvalidInput
	}
	return body, nil
}

type ToolEvidenceReader struct{ Store *Store }

func (reader ToolEvidenceReader) Evidence(ctx context.Context, logicalExecutionID string) (overview.ToolEvidence, error) {
	complete, err := reader.Store.ToolEvidence(ctx, logicalExecutionID)
	return overview.ToolEvidence{Complete: complete, Tools: []string{}, ExternalWriteAttempts: 0}, err
}

type InputHandler struct {
	store       *Store
	credentials *execution.RuntimeCredentialProvider
}

func NewInputHandler(store *Store, credentials *execution.RuntimeCredentialProvider) (http.Handler, error) {
	if store == nil || credentials == nil {
		return nil, ErrInvalidInput
	}
	return &InputHandler{store: store, credentials: credentials}, nil
}

func (handler *InputHandler) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		writer.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	agentID, err := handler.credentials.AgentForAuthorization(request.Header.Get("authorization"))
	if err != nil {
		writeInputError(writer, http.StatusUnauthorized, "unauthorized")
		return
	}
	inputRef := absoluteRequestURL(request)
	task, err := handler.store.InputAuthorized(request.Context(), agentID, inputRef)
	if err != nil {
		status := http.StatusServiceUnavailable
		if errors.Is(err, ErrForbidden) || errors.Is(err, ErrNotFound) {
			status = http.StatusForbidden
		}
		writeInputError(writer, status, "execution input unavailable")
		return
	}
	body, err := executionInput(task)
	if err != nil {
		writeInputError(writer, http.StatusServiceUnavailable, "execution input unavailable")
		return
	}
	writer.Header().Set("content-type", "application/json; charset=utf-8")
	writer.Header().Set("cache-control", "private, no-store")
	writer.Header().Set("content-length", fmt.Sprintf("%d", len(body)))
	writer.WriteHeader(http.StatusOK)
	_, _ = writer.Write(body)
}

func absoluteRequestURL(request *http.Request) string {
	scheme := request.Header.Get("x-forwarded-proto")
	if scheme == "" {
		scheme = "https"
	}
	host := request.Header.Get("x-forwarded-host")
	if host == "" {
		host = request.Host
	}
	return scheme + "://" + host + request.URL.EscapedPath()
}

func writeInputError(writer http.ResponseWriter, status int, message string) {
	writer.Header().Set("content-type", "application/json; charset=utf-8")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(map[string]string{"error": message})
}

func executionInput(task TaskInput) ([]byte, error) {
	expert := strings.ToLower(task.ExpertType)
	if strings.Contains(expert, "image-to-code") || strings.Contains(expert, "image to code") || strings.Contains(expert, "截图") {
		if len(task.Inputs) == 0 {
			return nil, ErrInvalidInput
		}
		var supplied map[string]any
		if json.Unmarshal([]byte(task.Inputs[0]), &supplied) != nil {
			return nil, ErrInvalidInput
		}
		image, exists := supplied["image"]
		if !exists {
			return nil, ErrInvalidInput
		}
		prompt := sanitizeOverviewText(task.Description, 1_000)
		if prompt == "" {
			prompt = sanitizeOverviewText(task.Title, 1_000)
		}
		return json.Marshal(map[string]any{"image": image, "prompt": prompt, "target": sanitizeOverviewText(task.DeliveryFormat, 200)})
	}
	prompt := sanitizeOverviewText(task.Description, 1_000)
	if prompt == "" {
		prompt = sanitizeOverviewText(task.Title, 1_000)
	}
	return json.Marshal(struct {
		Prompt  string `json:"prompt"`
		Size    string `json:"size"`
		Quality string `json:"quality"`
	}{Prompt: prompt, Size: "1280x1280", Quality: "hd"})
}

var overviewSensitiveText = regexp.MustCompile(`(?i)(bearer\s+\S+|(?:api[_-]?key|access[_-]?token|secret|password|private[_-]?key)\s*[:=]\s*\S+|[A-Z0-9._%+-]+@[A-Z0-9.-]+\.[A-Z]{2,})`)

func sanitizeOverviewText(value string, maximum int) string {
	value = strings.TrimSpace(overviewSensitiveText.ReplaceAllString(value, "[已脱敏]"))
	runes := []rune(value)
	if len(runes) > maximum {
		value = string(runes[:maximum])
	}
	return value
}

var _ overview.BriefProvider = (*BriefProvider)(nil)
var _ overview.TargetResolver = TargetResolver{}
var _ overview.ArtifactReader = (*ArtifactReader)(nil)
var _ overview.ToolEvidenceReader = ToolEvidenceReader{}
