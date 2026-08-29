package chain

import (
	"encoding/hex"
	"fmt"
	"math/big"
	"sort"
	"strings"

	"github.com/example/agent-platform/engine/internal/chain/taskescrowabi"
	"github.com/example/agent-platform/engine/internal/selection"
)

var eventSignatures = map[string]string{
	taskescrowabi.EventID("TaskCreated"):                EventTaskCreated,
	taskescrowabi.EventID("SelectionConfirmed"):         EventSelection,
	taskescrowabi.EventID("WorkNonceAdvanced"):          EventWorkNonce,
	taskescrowabi.EventID("FundsReleased"):              EventReleased,
	taskescrowabi.EventID("FundsRefunded"):              EventRefunded,
	taskescrowabi.EventID("EarningsAccrued"):            EventEarnings,
	taskescrowabi.EventID("EarningsWithdrawn"):          EventWithdrawal,
	taskescrowabi.EventID("YieldEligibilityChanged"):    EventYield,
	taskescrowabi.EventID("DisputeOpened"):              EventDisputeOpen,
	taskescrowabi.EventID("DisputeResolved"):            EventDisputeDone,
	taskescrowabi.EventID("DisputeFrozen"):              EventDisputeFreeze,
	taskescrowabi.EventID("DisputeAllocationFinalized"): EventDisputeAlloc,
}

var selectAgentSelector = taskescrowabi.MethodID("selectAgent")

func IsSelectionInput(input string) bool {
	return len(input) >= 10 && strings.EqualFold(input[:10], selectAgentSelector)
}

func SelectionCallSelector() string { return selectAgentSelector }

func DecodeBlock(scope Scope, block Block) ([]Event, error) {
	result := make([]Event, 0)
	for _, transaction := range block.Transactions {
		if strings.ToLower(transaction.To) != scope.Contract {
			continue
		}
		if transaction.Status != TxSucceeded && transaction.Status != TxFailed {
			return nil, ErrInvalidInput
		}
		var transactionProof *selection.Proof
		selectionCall := strings.HasPrefix(strings.ToLower(transaction.Input), selectAgentSelector)
		if selectionCall {
			proof, err := decodeSelectionInput(transaction.Input)
			if err != nil && transaction.Status == TxSucceeded {
				return nil, err
			}
			if err == nil {
				transactionProof = &proof
			}
		}
		selectionEvents := 0
		for _, log := range transaction.Logs {
			if strings.ToLower(log.Address) != scope.Contract || len(log.Topics) == 0 {
				continue
			}
			eventType, known := eventSignatures[strings.ToLower(log.Topics[0])]
			if !known {
				continue
			}
			if eventType == EventSelection {
				selectionEvents++
			}
			event, err := decodeEvent(block, transaction, log, eventType, transactionProof)
			if err != nil {
				return nil, err
			}
			result = append(result, event)
		}
		if selectionCall && ((transaction.Status == TxSucceeded && selectionEvents != 1) || (transaction.Status == TxFailed && selectionEvents != 0)) {
			return nil, fmt.Errorf("%w: selection receipt event cardinality", ErrInvalidInput)
		}
	}
	sort.Slice(result, func(left, right int) bool { return result[left].LogIndex < result[right].LogIndex })
	return result, nil
}

func decodeEvent(block Block, transaction Transaction, log Log, eventType string, proof *selection.Proof) (Event, error) {
	event := Event{ID: stableID("chain-event", block.Hash, transaction.Hash, fmt.Sprint(log.Index)), Type: eventType, BlockNumber: block.Number, BlockHash: block.Hash, TransactionHash: transaction.Hash, LogIndex: log.Index, Payload: map[string]any{}}
	data, err := words(log.Data)
	if err != nil {
		return Event{}, err
	}
	switch eventType {
	case EventTaskCreated:
		if len(log.Topics) != 3 || len(data) != 1 {
			return Event{}, ErrInvalidInput
		}
		event.TaskID = hashTopic(log.Topics[1])
		event.Payload = map[string]any{"publisher": addressTopic(log.Topics[2]), "amount": number(data[0])}
	case EventSelection:
		if len(log.Topics) != 4 || len(data) != 8 || proof == nil {
			return Event{}, ErrInvalidInput
		}
		event.TaskID, event.AssignmentID = hashTopic(log.Topics[1]), hashTopic(log.Topics[2])
		controller := addressTopic(log.Topics[3])
		formalPayable, workNonce := number(data[5]), uint64Number(data[7])
		if event.TaskID != proof.TaskID || event.AssignmentID != proof.AssignmentID || controller != proof.AgentController || address(data[0]) != proof.Payout || hashWord(data[1]) != proof.OverviewID || hashWord(data[2]) != proof.AllocationID || number(data[3]) != proof.FormalGrossPrice || number(data[4]) != proof.OverviewCredit || subtract(proof.FormalGrossPrice, proof.OverviewCredit) != formalPayable || workNonce != 1 {
			return Event{}, fmt.Errorf("%w: selection calldata and log mismatch", ErrInvalidInput)
		}
		event.Payload = map[string]any{"payout": proof.Payout, "overviewId": proof.OverviewID, "allocationId": proof.AllocationID, "formalGrossPrice": proof.FormalGrossPrice, "overviewCredit": proof.OverviewCredit, "formalPayable": formalPayable, "excessRefunded": number(data[6]), "workNonce": workNonce}
		event.Selection = &selection.ChainResult{Status: selection.ChainConfirmed, TransactionHash: transaction.Hash, BlockNumber: block.Number, LogIndex: log.Index, Proof: *proof, FormalPayable: formalPayable, WorkNonce: workNonce}
	case EventWorkNonce:
		if len(log.Topics) != 3 || len(data) != 1 {
			return Event{}, ErrInvalidInput
		}
		event.TaskID, event.AssignmentID = hashTopic(log.Topics[1]), hashTopic(log.Topics[2])
		event.Payload = map[string]any{"workNonce": uint64Number(data[0])}
	case EventReleased, EventRefunded:
		if len(log.Topics) != 3 || len(data) != 1 {
			return Event{}, ErrInvalidInput
		}
		event.TaskID = hashTopic(log.Topics[1])
		event.Payload = map[string]any{"recipient": addressTopic(log.Topics[2]), "amount": number(data[0])}
	case EventEarnings:
		if len(log.Topics) != 4 || len(data) != 2 {
			return Event{}, ErrInvalidInput
		}
		event.TaskID, event.AssignmentID = hashTopic(log.Topics[1]), hashTopic(log.Topics[2])
		event.Payload = map[string]any{"agentController": addressTopic(log.Topics[3]), "payout": address(data[0]), "amount": number(data[1])}
	case EventWithdrawal:
		if len(log.Topics) != 3 || len(data) != 1 {
			return Event{}, ErrInvalidInput
		}
		event.Payload = map[string]any{"agentController": addressTopic(log.Topics[1]), "payout": addressTopic(log.Topics[2]), "amount": number(data[0])}
	case EventYield:
		if len(log.Topics) != 2 || len(data) != 2 || (number(data[1]) != "0" && number(data[1]) != "1") {
			return Event{}, ErrInvalidInput
		}
		event.TaskID = hashTopic(log.Topics[1])
		event.Payload = map[string]any{"amount": number(data[0]), "eligible": number(data[1]) == "1"}
	case EventDisputeOpen:
		if len(log.Topics) != 3 || len(data) != 0 {
			return Event{}, ErrInvalidInput
		}
		event.TaskID = hashTopic(log.Topics[1])
		event.Payload = map[string]any{"openedBy": addressTopic(log.Topics[2])}
	case EventDisputeDone:
		if len(log.Topics) != 3 || len(data) != 1 {
			return Event{}, ErrInvalidInput
		}
		event.TaskID = hashTopic(log.Topics[1])
		event.Payload = map[string]any{"recipient": addressTopic(log.Topics[2]), "amount": number(data[0])}
	case EventDisputeFreeze:
		if len(log.Topics) != 3 || len(data) != 4 {
			return Event{}, ErrInvalidInput
		}
		event.TaskID = hashTopic(log.Topics[1])
		event.Payload = map[string]any{"root": hashTopic(log.Topics[2]), "leafCount": uint64Number(data[0]), "amount": number(data[1]), "feeCap": number(data[2]), "finalizeAfter": uint64Number(data[3])}
	case EventDisputeAlloc:
		if len(log.Topics) != 3 || len(data) != 3 {
			return Event{}, ErrInvalidInput
		}
		event.TaskID = hashTopic(log.Topics[1])
		event.Payload = map[string]any{"root": hashTopic(log.Topics[2]), "publisherAmount": number(data[0]), "agentAmount": number(data[1]), "feeAmount": number(data[2])}
	}
	return event, nil
}

func decodeSelectionInput(input string) (selection.Proof, error) {
	input = strings.ToLower(input)
	if !strings.HasPrefix(input, selectAgentSelector) {
		return selection.Proof{}, ErrInvalidInput
	}
	raw, err := hex.DecodeString(input[10:])
	if err != nil || len(raw) < 17*32 {
		return selection.Proof{}, ErrInvalidInput
	}
	word := func(index int) []byte { return raw[index*32 : (index+1)*32] }
	proof := selection.Proof{
		TaskID: hashWord(word(0)), AssignmentID: hashWord(word(1)), AgentController: address(word(2)), Payout: address(word(3)),
		OverviewID: hashWord(word(4)), AllocationID: hashWord(word(5)), QuoteHash: hashWord(word(6)), TaskSpecHash: hashWord(word(7)),
		MatchRevision: uint64Number(word(8)), PriceVersion: uint64Number(word(9)), OverviewPrice: number(word(10)), FormalGrossPrice: number(word(11)), OverviewCredit: number(word(12)),
		PolicyHash: hashWord(word(13)), Nonce: hashWord(word(14)), Deadline: uint64Number(word(15)),
	}
	offset := new(big.Int).SetBytes(word(16))
	if !offset.IsUint64() || offset.Uint64() != 17*32 || proof.MatchRevision == 0 || proof.PriceVersion == 0 || proof.Deadline == 0 {
		return selection.Proof{}, ErrInvalidInput
	}
	return proof, nil
}

func words(value string) ([][]byte, error) {
	if !strings.HasPrefix(value, "0x") {
		return nil, ErrInvalidInput
	}
	raw, err := hex.DecodeString(value[2:])
	if err != nil || len(raw)%32 != 0 {
		return nil, ErrInvalidInput
	}
	result := make([][]byte, len(raw)/32)
	for index := range result {
		result[index] = raw[index*32 : (index+1)*32]
	}
	return result, nil
}

func hashTopic(value string) string {
	if !validHash(strings.ToLower(value)) {
		return ""
	}
	return strings.ToLower(value)
}
func addressTopic(value string) string {
	raw, err := hex.DecodeString(strings.TrimPrefix(value, "0x"))
	if err != nil || len(raw) != 32 {
		return ""
	}
	return address(raw)
}
func hashWord(value []byte) string { return "0x" + hex.EncodeToString(value) }
func address(value []byte) string {
	if len(value) != 32 {
		return ""
	}
	return "0x" + hex.EncodeToString(value[12:])
}
func number(value []byte) string { return new(big.Int).SetBytes(value).String() }
func uint64Number(value []byte) uint64 {
	number := new(big.Int).SetBytes(value)
	if !number.IsUint64() {
		return 0
	}
	return number.Uint64()
}
func subtract(left, right string) string {
	l, lok := new(big.Int).SetString(left, 10)
	r, rok := new(big.Int).SetString(right, 10)
	if !lok || !rok || l.Cmp(r) < 0 {
		return ""
	}
	return l.Sub(l, r).String()
}
