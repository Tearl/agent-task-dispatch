package chain

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRPCSourceValidatesNetworkAndDecodesAuthoritativeResponses(t *testing.T) {
	txHash := hash("rpc-transaction")
	blockHash := hash("rpc-block")
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var call struct {
			ID     uint64            `json:"id"`
			Method string            `json:"method"`
			Params []json.RawMessage `json:"params"`
		}
		if err := json.NewDecoder(request.Body).Decode(&call); err != nil {
			t.Error(err)
			return
		}
		var result any
		switch call.Method {
		case "eth_chainId":
			result = "0x7a69"
		case "eth_blockNumber":
			result = "0x3"
		case "eth_getBlockByNumber":
			result = map[string]any{"number": "0x2", "hash": blockHash, "parentHash": hash("rpc-parent"), "timestamp": "0x64", "transactions": []map[string]string{{"hash": txHash, "to": testContract, "input": SelectionCallSelector()}}}
		case "eth_getTransactionReceipt":
			result = map[string]any{"blockHash": blockHash, "status": "0x0", "logs": []any{}}
		default:
			t.Errorf("unexpected method %s", call.Method)
			return
		}
		_ = json.NewEncoder(writer).Encode(map[string]any{"jsonrpc": "2.0", "id": call.ID, "result": result})
	}))
	defer server.Close()
	if _, err := NewRPCSource(server.URL, testContract, false); err == nil {
		t.Fatal("private HTTP RPC was accepted without explicit opt-in")
	}
	source, err := NewRPCSource(server.URL, testContract, true)
	if err != nil {
		t.Fatal(err)
	}
	if chainID, err := source.ChainID(t.Context()); err != nil || chainID != "31337" {
		t.Fatalf("chain id=%s err=%v", chainID, err)
	}
	if head, err := source.Head(t.Context()); err != nil || head != 3 {
		t.Fatalf("head=%d err=%v", head, err)
	}
	block, err := source.Block(t.Context(), 2)
	if err != nil || block.Hash != blockHash || len(block.Transactions) != 1 || block.Transactions[0].Status != TxFailed {
		t.Fatalf("block=%#v err=%v", block, err)
	}
	if _, err = NewRPCSource(server.URL+"?token=secret", testContract, true); err == nil {
		t.Fatal("RPC query credentials were accepted")
	}
}

func TestRPCInventoryUsesHistoricalBlockTagAndContractGetters(t *testing.T) {
	proof := proofFixture()
	seen := make(map[string]bool)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var call struct {
			ID     uint64            `json:"id"`
			Method string            `json:"method"`
			Params []json.RawMessage `json:"params"`
		}
		_ = json.NewDecoder(request.Body).Decode(&call)
		if call.Method != "eth_call" || len(call.Params) != 2 || string(call.Params[1]) != `"0xc"` {
			t.Errorf("unexpected call %#v", call)
			return
		}
		var input map[string]string
		_ = json.Unmarshal(call.Params[0], &input)
		selectorValue := input["data"][:10]
		seen[selectorValue] = true
		var result string
		switch selectorValue {
		case selector("tasks(bytes32)"):
			result = encodeWords(addressWordTest("0x000000000000000000000000000000000000cafe"), addressWordTest(proof.Payout), decimalWord("90"), uintWordTest(2))
		case selector("assignments(bytes32)"):
			result = encodeWords(hexWord(proof.AssignmentID), addressWordTest(proof.AgentController), addressWordTest(proof.Payout), hexWord(proof.OverviewID), hexWord(proof.AllocationID), hexWord(proof.QuoteHash), hexWord(proof.TaskSpecHash), uintWordTest(1), uintWordTest(2), decimalWord("100"), decimalWord("10"), decimalWord("90"), hexWord(proof.PolicyHash))
		case selector("workNonces(bytes32)"):
			result = encodeWords(uintWordTest(1))
		case selector("yieldEligiblePrincipal(bytes32)"):
			result = encodeWords(decimalWord("90"))
		case selector("claimableEarnings(address,address)"):
			if len(input["data"]) != 10+64+64 {
				t.Errorf("claimable getter arguments were not ABI words: %s", input["data"])
			}
			result = encodeWords(decimalWord("50"))
		default:
			t.Errorf("unexpected selector %s", selectorValue)
			return
		}
		_ = json.NewEncoder(writer).Encode(map[string]any{"jsonrpc": "2.0", "id": call.ID, "result": result})
	}))
	defer server.Close()
	source, _ := NewRPCSource(server.URL, testContract, true)
	expected := map[string]string{"assignment:" + proof.TaskID: proof.AssignmentID, "task_amount:" + proof.TaskID: "90", "task_status:" + proof.TaskID: "2", "work_nonce:" + proof.TaskID: "1", "ledger_formal_balance:" + proof.TaskID: "90", "yield_principal:" + proof.TaskID: "90", "claimable:" + proof.AgentController + ":" + proof.Payout: "50", "ledger_claimable:" + proof.AgentController + ":" + proof.Payout: "50"}
	observed, err := source.ObservedInventory(t.Context(), testScope(), 12, expected)
	if err != nil || len(seen) != 5 || strings.Join([]string{observed["assignment:"+proof.TaskID], observed["task_amount:"+proof.TaskID], observed["task_status:"+proof.TaskID], observed["work_nonce:"+proof.TaskID], observed["ledger_formal_balance:"+proof.TaskID], observed["yield_principal:"+proof.TaskID], observed["claimable:"+proof.AgentController+":"+proof.Payout], observed["ledger_claimable:"+proof.AgentController+":"+proof.Payout]}, ",") != proof.AssignmentID+",90,2,1,90,90,50,50" {
		t.Fatalf("inventory=%#v seen=%#v err=%v", observed, seen, err)
	}
}
