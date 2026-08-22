package chain

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"testing"
	"time"

	"github.com/example/agent-platform/engine/internal/selection"
	"golang.org/x/crypto/sha3"
)

const testContract = "0x0000000000000000000000000000000000001234"

type sourceStub struct {
	chainID string
	head    uint64
	blocks  map[uint64]Block
}

func (source *sourceStub) ChainID(context.Context) (string, error) { return source.chainID, nil }
func (source *sourceStub) Head(context.Context) (uint64, error)    { return source.head, nil }
func (source *sourceStub) Block(_ context.Context, height uint64) (Block, error) {
	value, ok := source.blocks[height]
	if !ok {
		return Block{}, ErrGap
	}
	return value, nil
}

func TestProjectorReplaysRecoversCursorAndWaitsForConfirmations(t *testing.T) {
	source := linearSource(5, "a")
	repository := NewMemoryRepository()
	projector := newTestProjector(t, source, repository)
	cursor, err := projector.SyncOnce(context.Background())
	if err != nil || !cursor.Set || cursor.Height != 4 {
		t.Fatalf("first sync: %#v %v", cursor, err)
	}
	if replay, replayErr := projector.SyncOnce(context.Background()); replayErr != nil || replay != cursor {
		t.Fatalf("replay: %#v %v", replay, replayErr)
	}
	source.head = 6
	restarted := newTestProjector(t, source, repository)
	cursor, err = restarted.SyncOnce(context.Background())
	if err != nil || cursor.Height != 5 || len(repository.CanonicalHeights()) != 5 {
		t.Fatalf("cursor recovery: %#v %v", cursor, err)
	}
}

func TestProjectorReorgKeepsHistoryAndConvergesToNewCanonicalChain(t *testing.T) {
	source := linearSource(5, "old")
	repository := NewMemoryRepository()
	projector := newTestProjector(t, source, repository)
	if _, err := projector.SyncOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	block1 := source.blocks[1]
	source.blocks[2] = testBlock(2, block1.Hash, "new2")
	source.blocks[3] = testBlock(3, source.blocks[2].Hash, "new3")
	source.blocks[4] = testBlock(4, source.blocks[3].Hash, "new4")
	source.blocks[5] = testBlock(5, source.blocks[4].Hash, "new5")
	source.head = 6
	cursor, err := projector.SyncOnce(context.Background())
	if err != nil || cursor.Height != 5 || cursor.Hash != source.blocks[5].Hash {
		t.Fatalf("reorg sync: %#v %v", cursor, err)
	}
	if len(repository.blocks[2]) != 2 || repository.blocks[2][0].state != StateOrphaned || repository.blocks[2][1].state != StateCanonical {
		t.Fatalf("reorg history was not preserved: %#v", repository.blocks[2])
	}
}

func TestProjectorRejectsGapAndExcessiveReorg(t *testing.T) {
	source := linearSource(5, "old")
	repository := NewMemoryRepository()
	projector := newTestProjector(t, source, repository)
	if _, err := projector.SyncOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	source.blocks[5] = testBlock(6, hash("wrong"), "gap")
	source.head = 6
	if _, err := projector.SyncOnce(context.Background()); !errors.Is(err, ErrGap) {
		t.Fatalf("gap returned %v", err)
	}

	deepSource := linearSource(5, "deep-old")
	deepRepository := NewMemoryRepository()
	deepScope := testScope()
	deepScope.MaxReorgDepth = 2
	deepProjector, _ := NewProjector(deepSource, deepRepository, deepScope)
	if _, err := deepProjector.SyncOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	parent := hash("foreign-genesis")
	for height := uint64(1); height <= 5; height++ {
		deepSource.blocks[height] = testBlock(height, parent, fmt.Sprintf("deep-new-%d", height))
		parent = deepSource.blocks[height].Hash
	}
	deepSource.head = 6
	if _, err := deepProjector.SyncOnce(context.Background()); !errors.Is(err, ErrReorgTooDeep) {
		t.Fatalf("deep reorg returned %v", err)
	}
}

func TestSelectionProjectionRequiresMatchingReceiptLogAndCalldata(t *testing.T) {
	if SelectionCallSelector() != "0xa2dfc191" || selector("tasks(bytes32)") != "0xe579f500" || selector("assignments(bytes32)") != "0x82c2901c" || selector("workNonces(bytes32)") != "0x1f0a5c53" {
		t.Fatal("TaskEscrow ABI selectors changed")
	}
	proof := proofFixture()
	txHash := hash("selection-tx")
	transaction := Transaction{Hash: txHash, To: testContract, Input: encodeSelectionInput(proof), Status: TxSucceeded, Logs: []Log{selectionLog(proof, 7)}}
	block := testBlock(1, hash("genesis"), "selection")
	block.Transactions = []Transaction{transaction}
	events, err := DecodeBlock(testScope(), block)
	if err != nil || len(events) != 1 || events[0].Selection == nil {
		t.Fatalf("decode: %#v %v", events, err)
	}
	repository := NewMemoryRepository()
	if err = repository.ApplyBlock(context.Background(), testScope(), block, events); err != nil {
		t.Fatal(err)
	}
	verifier, _ := NewVerifier(repository, testScope())
	result, err := verifier.VerifySelection(context.Background(), txHash)
	if err != nil || result.Status != selection.ChainConfirmed || result.Proof != proof || result.FormalPayable != "90" || result.WorkNonce != 1 {
		t.Fatalf("verification: %#v %v", result, err)
	}

	tampered := transaction
	tampered.Logs = []Log{selectionLog(proof, 8)}
	words, _ := words(tampered.Logs[0].Data)
	words[5] = uintWordTest(91)
	tampered.Logs[0].Data = encodeWords(words...)
	block.Transactions = []Transaction{tampered}
	if _, err = DecodeBlock(testScope(), block); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("tampered event returned %v", err)
	}
}

func TestFailedSelectionAndOrphanedConfirmationConverge(t *testing.T) {
	proof := proofFixture()
	txHash := hash("selection-failed")
	failedBlock := testBlock(1, hash("genesis"), "failed")
	failedBlock.Transactions = []Transaction{{Hash: txHash, To: testContract, Input: encodeSelectionInput(proof), Status: TxFailed}}
	repository := NewMemoryRepository()
	if err := repository.ApplyBlock(context.Background(), testScope(), failedBlock, nil); err != nil {
		t.Fatal(err)
	}
	verifier, _ := NewVerifier(repository, testScope())
	result, err := verifier.VerifySelection(context.Background(), txHash)
	if err != nil || result.Status != selection.ChainFailed {
		t.Fatalf("failed selection: %#v %v", result, err)
	}
	if err = repository.Rewind(context.Background(), testScope(), 0, "chain_reorganization"); err != nil {
		t.Fatal(err)
	}
	result, err = verifier.VerifySelection(context.Background(), txHash)
	if err != nil || result.Status != selection.ChainPending {
		t.Fatalf("orphaned transaction remained authoritative: %#v %v", result, err)
	}
}

func TestDecoderSortsOutOfOrderLogsByCanonicalLogIndex(t *testing.T) {
	taskA, taskB := hash("task-a"), hash("task-b")
	topic := hexKeccak("TaskCreated(bytes32,address,uint256)")
	publisher := "0x" + strings.Repeat("0", 24) + strings.Repeat("1", 40)
	block := testBlock(1, hash("genesis"), "log-order")
	block.Transactions = []Transaction{{Hash: hash("creation"), To: testContract, Input: "0x", Status: TxSucceeded, Logs: []Log{
		{Index: 9, Address: testContract, Topics: []string{topic, taskB, publisher}, Data: encodeWords(uintWordTest(2))},
		{Index: 3, Address: testContract, Topics: []string{topic, taskA, publisher}, Data: encodeWords(uintWordTest(1))},
	}}}
	events, err := DecodeBlock(testScope(), block)
	if err != nil || len(events) != 2 || events[0].LogIndex != 3 || events[1].LogIndex != 9 {
		t.Fatalf("event order=%#v err=%v", events, err)
	}
}

func TestDecoderProjectsEarningsWithdrawalAndYieldIsolationEvents(t *testing.T) {
	proof := proofFixture()
	controllerTopic := "0x" + strings.Repeat("0", 24) + strings.TrimPrefix(proof.AgentController, "0x")
	payoutTopic := "0x" + strings.Repeat("0", 24) + strings.TrimPrefix(proof.Payout, "0x")
	block := testBlock(1, hash("genesis"), "settlement-events")
	block.Transactions = []Transaction{{Hash: hash("settlement"), To: testContract, Input: "0x", Status: TxSucceeded, Logs: []Log{
		{Index: 1, Address: testContract, Topics: []string{hexKeccak("YieldEligibilityChanged(bytes32,uint256,bool)"), proof.TaskID}, Data: encodeWords(decimalWord("90"), uintWordTest(1))},
		{Index: 2, Address: testContract, Topics: []string{hexKeccak("EarningsAccrued(bytes32,bytes32,address,address,uint256)"), proof.TaskID, proof.AssignmentID, controllerTopic}, Data: encodeWords(addressWordTest(proof.Payout), decimalWord("90"))},
		{Index: 3, Address: testContract, Topics: []string{hexKeccak("EarningsWithdrawn(address,address,uint256)"), controllerTopic, payoutTopic}, Data: encodeWords(decimalWord("40"))},
	}}}
	events, err := DecodeBlock(testScope(), block)
	if err != nil || len(events) != 3 {
		t.Fatalf("decode settlement events: %#v %v", events, err)
	}
	if events[0].Type != EventYield || events[0].Payload["eligible"] != true || events[0].Payload["amount"] != "90" {
		t.Fatalf("yield projection mismatch: %#v", events[0])
	}
	if events[1].Type != EventEarnings || events[1].AssignmentID != proof.AssignmentID || events[1].Payload["agentController"] != proof.AgentController || events[1].Payload["payout"] != proof.Payout {
		t.Fatalf("earnings projection mismatch: %#v", events[1])
	}
	if events[2].Type != EventWithdrawal || events[2].TaskID != "" || events[2].Payload["amount"] != "40" {
		t.Fatalf("withdrawal projection mismatch: %#v", events[2])
	}

	invalid := block
	invalid.Transactions[0].Logs = append([]Log(nil), block.Transactions[0].Logs...)
	invalid.Transactions[0].Logs[0].Data = encodeWords(decimalWord("90"), uintWordTest(2))
	if _, err = DecodeBlock(testScope(), invalid); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("non-boolean yield flag returned %v", err)
	}
}

type inventoryStub map[string]string

func (inventory inventoryStub) ObservedInventory(context.Context, Scope, uint64, map[string]string) (map[string]string, error) {
	return cloneMap(inventory), nil
}

func TestReconciliationDetectsEveryInjectedDifferenceDeterministically(t *testing.T) {
	repository := NewMemoryRepository()
	block := testBlock(1, hash("genesis"), "inventory")
	if err := repository.ApplyBlock(context.Background(), testScope(), block, nil); err != nil {
		t.Fatal(err)
	}
	repository.SetExpectedInventory(map[string]string{"assignment:task-a": "a1", "task_amount:task-a": "90", "work_nonce:task-a": "1"})
	reconciler, _ := NewReconciler(repository, inventoryStub{"assignment:task-a": "wrong", "task_amount:task-a": "91", "receivable:agent-a": "10"}, testScope())
	reconciler.now = func() time.Time { return time.Unix(1_800_000_000, 0).UTC() }
	run, err := reconciler.Run(context.Background())
	if err != nil || run.Status != "difference_detected" || len(run.Differences) != 4 {
		t.Fatalf("reconciliation: %#v %v", run, err)
	}
	if run.Differences[0].Category != "assignment" || run.Differences[3].Category != "work_nonce" {
		t.Fatalf("differences are not stable: %#v", run.Differences)
	}
}

func newTestProjector(t *testing.T, source Source, repository Repository) *Projector {
	t.Helper()
	value, err := NewProjector(source, repository, testScope())
	if err != nil {
		t.Fatal(err)
	}
	return value
}
func testScope() Scope {
	return Scope{ChainID: "31337", Contract: testContract, StartBlock: 1, Confirmations: 2, MaxReorgDepth: 10}
}
func linearSource(head uint64, prefix string) *sourceStub {
	source := &sourceStub{chainID: "31337", head: head, blocks: make(map[uint64]Block)}
	parent := hash("genesis")
	for height := uint64(1); height <= head; height++ {
		block := testBlock(height, parent, fmt.Sprintf("%s-%d", prefix, height))
		source.blocks[height] = block
		parent = block.Hash
	}
	return source
}
func testBlock(height uint64, parent, identity string) Block {
	return Block{Number: height, Hash: hash(identity), ParentHash: parent, Timestamp: time.Unix(int64(height), 0).UTC()}
}
func hash(value string) string { raw := sha3Digest(value); return "0x" + hex.EncodeToString(raw) }
func sha3Digest(value string) []byte {
	h := sha3.NewLegacyKeccak256()
	_, _ = h.Write([]byte(value))
	return h.Sum(nil)
}

func proofFixture() selection.Proof {
	return selection.Proof{TaskID: hash("task"), AssignmentID: hash("assignment"), AgentController: "0x000000000000000000000000000000000000beef", Payout: "0x000000000000000000000000000000000000f00d", OverviewID: hash("overview"), AllocationID: hash("allocation"), QuoteHash: hash("quote"), TaskSpecHash: hash("spec"), MatchRevision: 1, PriceVersion: 2, OverviewPrice: "10", FormalGrossPrice: "100", OverviewCredit: "10", PolicyHash: hash("policy"), Nonce: hash("nonce"), Deadline: 1_800_000_000}
}

func encodeSelectionInput(proof selection.Proof) string {
	values := [][]byte{hexWord(proof.TaskID), hexWord(proof.AssignmentID), addressWordTest(proof.AgentController), addressWordTest(proof.Payout), hexWord(proof.OverviewID), hexWord(proof.AllocationID), hexWord(proof.QuoteHash), hexWord(proof.TaskSpecHash), uintWordTest(proof.MatchRevision), uintWordTest(proof.PriceVersion), decimalWord(proof.OverviewPrice), decimalWord(proof.FormalGrossPrice), decimalWord(proof.OverviewCredit), hexWord(proof.PolicyHash), hexWord(proof.Nonce), uintWordTest(proof.Deadline), uintWordTest(17 * 32), uintWordTest(65), make([]byte, 32), make([]byte, 32), make([]byte, 32)}
	return selectAgentSelector + strings.TrimPrefix(encodeWords(values...), "0x")
}

func selectionLog(proof selection.Proof, index uint) Log {
	data := encodeWords(addressWordTest(proof.Payout), hexWord(proof.OverviewID), hexWord(proof.AllocationID), decimalWord(proof.FormalGrossPrice), decimalWord(proof.OverviewCredit), decimalWord("90"), uintWordTest(0), uintWordTest(1))
	return Log{Index: index, Address: testContract, Topics: []string{hexKeccak("SelectionConfirmed(bytes32,bytes32,address,address,bytes32,bytes32,uint256,uint256,uint256,uint256,uint256)"), proof.TaskID, proof.AssignmentID, "0x" + strings.Repeat("0", 24) + strings.TrimPrefix(proof.AgentController, "0x")}, Data: data}
}

func hexWord(value string) []byte {
	raw, _ := hex.DecodeString(strings.TrimPrefix(value, "0x"))
	return raw
}
func addressWordTest(value string) []byte { return append(make([]byte, 12), hexWord(value)...) }
func uintWordTest[T ~uint64 | ~int](value T) []byte {
	result := make([]byte, 32)
	new(big.Int).SetUint64(uint64(value)).FillBytes(result)
	return result
}
func decimalWord(value string) []byte {
	number, _ := new(big.Int).SetString(value, 10)
	result := make([]byte, 32)
	number.FillBytes(result)
	return result
}
func encodeWords(values ...[]byte) string {
	var builder strings.Builder
	builder.WriteString("0x")
	for _, value := range values {
		builder.WriteString(hex.EncodeToString(value))
	}
	return builder.String()
}
