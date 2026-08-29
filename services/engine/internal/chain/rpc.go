package chain

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/example/agent-platform/engine/internal/chain/taskescrowabi"
)

type RPCSource struct {
	endpoint string
	contract string
	client   *http.Client
	nextID   atomic.Uint64
}

func NewRPCSource(endpoint, contract string, allowPrivateHTTP bool) (*RPCSource, error) {
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || (parsed.Scheme != "https" && !(allowPrivateHTTP && parsed.Scheme == "http")) || parsed.Hostname() == "" || !validAddress(strings.ToLower(contract)) {
		return nil, ErrInvalidInput
	}
	if address := net.ParseIP(parsed.Hostname()); address != nil && !allowPrivateHTTP && (address.IsPrivate() || address.IsLoopback() || address.IsUnspecified() || address.IsLinkLocalUnicast()) {
		return nil, ErrInvalidInput
	}
	client := &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("RPC redirects are disabled") }}
	return &RPCSource{endpoint: endpoint, contract: strings.ToLower(contract), client: client}, nil
}

func (source *RPCSource) ChainID(ctx context.Context) (string, error) {
	var result string
	if err := source.call(ctx, "eth_chainId", []any{}, &result); err != nil {
		return "", err
	}
	value, ok := new(big.Int).SetString(strings.TrimPrefix(result, "0x"), 16)
	if !ok || value.Sign() <= 0 {
		return "", ErrInvalidInput
	}
	return value.String(), nil
}

func (source *RPCSource) Head(ctx context.Context) (uint64, error) {
	var result string
	if err := source.call(ctx, "eth_blockNumber", []any{}, &result); err != nil {
		return 0, err
	}
	return parseQuantity(result)
}

func (source *RPCSource) Block(ctx context.Context, height uint64) (Block, error) {
	var raw rpcBlock
	if err := source.call(ctx, "eth_getBlockByNumber", []any{quantity(height), true}, &raw); err != nil {
		return Block{}, err
	}
	if raw.Hash == "" {
		return Block{}, ErrGap
	}
	numberValue, err := parseQuantity(raw.Number)
	if err != nil || numberValue != height {
		return Block{}, ErrGap
	}
	timestamp, err := parseQuantity(raw.Timestamp)
	if err != nil {
		return Block{}, err
	}
	block := Block{Number: height, Hash: strings.ToLower(raw.Hash), ParentHash: strings.ToLower(raw.ParentHash), Timestamp: time.Unix(int64(timestamp), 0).UTC()}
	for _, transaction := range raw.Transactions {
		if strings.ToLower(transaction.To) != source.contract {
			continue
		}
		var receipt rpcReceipt
		if err = source.call(ctx, "eth_getTransactionReceipt", []any{transaction.Hash}, &receipt); err != nil {
			return Block{}, err
		}
		if !strings.EqualFold(receipt.BlockHash, raw.Hash) {
			return Block{}, ErrGap
		}
		status := TxFailed
		if receipt.Status == "0x1" {
			status = TxSucceeded
		} else if receipt.Status != "0x0" {
			return Block{}, ErrInvalidInput
		}
		projected := Transaction{Hash: strings.ToLower(transaction.Hash), To: source.contract, Input: strings.ToLower(transaction.Input), Status: status}
		for _, log := range receipt.Logs {
			index, parseErr := parseQuantity(log.LogIndex)
			if parseErr != nil || index > uint64(^uint(0)) {
				return Block{}, ErrInvalidInput
			}
			topics := make([]string, len(log.Topics))
			for i := range log.Topics {
				topics[i] = strings.ToLower(log.Topics[i])
			}
			projected.Logs = append(projected.Logs, Log{Index: uint(index), Address: strings.ToLower(log.Address), Topics: topics, Data: strings.ToLower(log.Data)})
		}
		block.Transactions = append(block.Transactions, projected)
	}
	return block, nil
}

func (source *RPCSource) ObservedInventory(ctx context.Context, scope Scope, height uint64, expected map[string]string) (map[string]string, error) {
	result := make(map[string]string, len(expected))
	tasks := make(map[string]struct{})
	for key := range expected {
		if _, taskID, ok := strings.Cut(key, ":"); ok && validHash(taskID) {
			tasks[taskID] = struct{}{}
		}
	}
	for taskID := range tasks {
		taskWords, err := source.ethCall(ctx, taskescrowabi.MethodID("tasks"), taskID, height)
		if err != nil || len(taskWords) != 4 {
			return nil, errors.Join(ErrInvalidInput, err)
		}
		assignmentWords, err := source.ethCall(ctx, taskescrowabi.MethodID("assignments"), taskID, height)
		if err != nil || len(assignmentWords) != 13 {
			return nil, errors.Join(ErrInvalidInput, err)
		}
		nonceWords, err := source.ethCall(ctx, taskescrowabi.MethodID("workNonces"), taskID, height)
		if err != nil || len(nonceWords) != 1 {
			return nil, errors.Join(ErrInvalidInput, err)
		}
		result["assignment:"+taskID] = hashWord(assignmentWords[0])
		result["task_amount:"+taskID] = number(taskWords[2])
		result["task_status:"+taskID] = number(taskWords[3])
		result["work_nonce:"+taskID] = number(nonceWords[0])
		if _, exists := expected["ledger_formal_balance:"+taskID]; exists {
			result["ledger_formal_balance:"+taskID] = number(taskWords[2])
		}
		if _, exists := expected["yield_principal:"+taskID]; exists {
			yieldWords, callErr := source.ethCall(ctx, taskescrowabi.MethodID("yieldEligiblePrincipal"), taskID, height)
			if callErr != nil || len(yieldWords) != 1 {
				return nil, errors.Join(ErrInvalidInput, callErr)
			}
			result["yield_principal:"+taskID] = number(yieldWords[0])
		}
	}
	for key := range expected {
		parts := strings.Split(key, ":")
		if len(parts) != 3 || (parts[0] != "claimable" && parts[0] != "ledger_claimable") || !validAddress(parts[1]) || !validAddress(parts[2]) {
			continue
		}
		words, err := source.ethCallArguments(ctx, taskescrowabi.MethodID("claimableEarnings"), []string{
			strings.Repeat("0", 24) + strings.TrimPrefix(parts[1], "0x"),
			strings.Repeat("0", 24) + strings.TrimPrefix(parts[2], "0x"),
		}, height)
		if err != nil || len(words) != 1 {
			return nil, errors.Join(ErrInvalidInput, err)
		}
		result[key] = number(words[0])
	}
	return result, nil
}

func (source *RPCSource) ethCall(ctx context.Context, methodSelector, taskID string, height uint64) ([][]byte, error) {
	return source.ethCallArguments(ctx, methodSelector, []string{strings.TrimPrefix(taskID, "0x")}, height)
}

func (source *RPCSource) ethCallArguments(ctx context.Context, methodSelector string, arguments []string, height uint64) ([][]byte, error) {
	var result string
	data := methodSelector + strings.Join(arguments, "")
	if err := source.call(ctx, "eth_call", []any{map[string]string{"to": source.contract, "data": data}, quantity(height)}, &result); err != nil {
		return nil, err
	}
	return words(strings.ToLower(result))
}

func (source *RPCSource) call(ctx context.Context, method string, params []any, output any) error {
	requestID := source.nextID.Add(1)
	requestBody, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": requestID, "method": method, "params": params})
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, source.endpoint, bytes.NewReader(requestBody))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := source.client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 64<<10))
		return fmt.Errorf("RPC HTTP status %d", response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, (2<<20)+1))
	if err != nil || len(body) > 2<<20 {
		return ErrInvalidInput
	}
	var envelope struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      uint64          `json:"id"`
		Result  json.RawMessage `json:"result"`
		Error   *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err = json.Unmarshal(body, &envelope); err != nil || envelope.JSONRPC != "2.0" || envelope.ID != requestID {
		return ErrInvalidInput
	}
	if envelope.Error != nil {
		return fmt.Errorf("RPC error %d", envelope.Error.Code)
	}
	if len(envelope.Result) == 0 || string(envelope.Result) == "null" {
		return ErrGap
	}
	return json.Unmarshal(envelope.Result, output)
}

type rpcBlock struct {
	Number       string `json:"number"`
	Hash         string `json:"hash"`
	ParentHash   string `json:"parentHash"`
	Timestamp    string `json:"timestamp"`
	Transactions []struct {
		Hash  string `json:"hash"`
		To    string `json:"to"`
		Input string `json:"input"`
	} `json:"transactions"`
}
type rpcReceipt struct {
	BlockHash string `json:"blockHash"`
	Status    string `json:"status"`
	Logs      []struct {
		LogIndex string   `json:"logIndex"`
		Address  string   `json:"address"`
		Topics   []string `json:"topics"`
		Data     string   `json:"data"`
	} `json:"logs"`
}

func parseQuantity(value string) (uint64, error) {
	if !strings.HasPrefix(value, "0x") || len(value) < 3 {
		return 0, ErrInvalidInput
	}
	parsed, err := strconv.ParseUint(value[2:], 16, 64)
	return parsed, err
}
func quantity(value uint64) string { return fmt.Sprintf("0x%x", value) }

var _ Source = (*RPCSource)(nil)
var _ InventorySource = (*RPCSource)(nil)
