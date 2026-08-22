// SPDX-License-Identifier: MIT
pragma solidity ^0.8.24;

import {TaskEscrow} from "../src/TaskEscrow.sol";

interface Vm {
    function addr(uint256 privateKey) external returns (address);
    function chainId(uint256 newChainId) external;
    function deal(address account, uint256 newBalance) external;
    function etch(address target, bytes calldata code) external;
    function expectRevert(bytes4 selector) external;
    function prank(address sender) external;
    function sign(uint256 privateKey, bytes32 digest) external returns (uint8 v, bytes32 r, bytes32 s);
}

contract ReentrantPublisher {
    TaskEscrow private immutable escrow;
    TaskEscrow.SelectionProof private pendingProof;
    bytes private pendingSignature;
    bool public reentrySucceeded;
    uint256 public reentryAttempts;

    constructor(TaskEscrow target) {
        escrow = target;
    }

    function fund(bytes32 taskId) external payable {
        escrow.createTask{value: msg.value}(taskId);
    }

    function select(TaskEscrow.SelectionProof calldata proof, bytes calldata signature) external {
        pendingProof = proof;
        pendingSignature = signature;
        escrow.selectAgent(proof, signature);
    }

    receive() external payable {
        reentryAttempts++;
        TaskEscrow.SelectionProof memory proof = pendingProof;
        bytes memory signature = pendingSignature;
        (reentrySucceeded,) =
            address(escrow).call(abi.encodeWithSelector(TaskEscrow.selectAgent.selector, proof, signature));
    }
}

contract ReentrantAgent {
    TaskEscrow private immutable escrow;
    bool public reentrySucceeded;
    uint256 public reentryAttempts;

    constructor(TaskEscrow target) {
        escrow = target;
    }

    function withdraw(uint256 amount) external {
        escrow.withdrawEarnings(payable(address(this)), amount);
    }

    receive() external payable {
        reentryAttempts++;
        (reentrySucceeded,) = address(escrow).call(
            abi.encodeWithSelector(TaskEscrow.withdrawEarnings.selector, payable(address(this)), 1)
        );
    }
}

contract TaskEscrowTest {
    Vm private constant vm = Vm(address(uint160(uint256(keccak256("hevm cheat code")))));
    uint256 private constant PLATFORM_SIGNER_KEY = 0xA11CE;
    address private constant publisher = address(0xCAFE);
    address private constant agentController = address(0xBEEF);
    address private constant payout = address(0xF00D);
    TaskEscrow private escrow;

    function setUp() public {
        escrow = new TaskEscrow(address(this), vm.addr(PLATFORM_SIGNER_KEY));
        vm.deal(publisher, 100 ether);
    }

    function testSelectionAppliesCreditLocksNetRefundsExcessAndStartsV1() public {
        bytes32 taskId = keccak256("task-selection");
        _fund(taskId, 8 ether);
        TaskEscrow.SelectionProof memory proof = _proof(taskId, 6 ether, 2 ether, 2 ether);
        bytes memory signature = _sign(proof);
        uint256 publisherBefore = publisher.balance;
        vm.prank(publisher);
        escrow.selectAgent(proof, signature);

        (address storedPublisher, address storedAgent, uint256 amount, TaskEscrow.Status status) = escrow.tasks(taskId);
        require(storedPublisher == publisher && storedAgent == payout, "identity binding changed");
        require(amount == 4 ether && status == TaskEscrow.Status.Assigned, "wrong formal lock");
        require(publisher.balance == publisherBefore + 4 ether, "excess was not refunded");
        require(address(escrow).balance == 4 ether, "escrow value was not conserved");
        require(escrow.workNonces(taskId) == 1, "V1 work nonce was not initialized");
        require(escrow.assignmentIdOf(taskId) == proof.assignmentId, "assignment id changed");
    }

    function testSelectionReplayProducesOnlyOneAssignment() public {
        bytes32 taskId = keccak256("task-replay");
        _fund(taskId, 4 ether);
        TaskEscrow.SelectionProof memory proof = _proof(taskId, 4 ether, 1 ether, 1 ether);
        bytes memory signature = _sign(proof);
        vm.prank(publisher);
        escrow.selectAgent(proof, signature);
        vm.expectRevert(TaskEscrow.InvalidState.selector);
        vm.prank(publisher);
        escrow.selectAgent(proof, signature);
    }

    function testNonceCannotBeReusedAcrossTasks() public {
        bytes32 firstTask = keccak256("task-first");
        bytes32 secondTask = keccak256("task-second");
        _fund(firstTask, 4 ether);
        _fund(secondTask, 4 ether);
        TaskEscrow.SelectionProof memory first = _proof(firstTask, 4 ether, 1 ether, 1 ether);
        TaskEscrow.SelectionProof memory second = _proof(secondTask, 4 ether, 1 ether, 1 ether);
        second.nonce = first.nonce;
        bytes memory firstSignature = _sign(first);
        bytes memory secondSignature = _sign(second);
        vm.prank(publisher);
        escrow.selectAgent(first, firstSignature);
        vm.expectRevert(TaskEscrow.InvalidNonce.selector);
        vm.prank(publisher);
        escrow.selectAgent(second, secondSignature);
    }

    function testAssignmentAndOverviewCreditCannotBeReusedAcrossTasks() public {
        bytes32 firstTask = keccak256("task-binding-first");
        bytes32 secondTask = keccak256("task-binding-second");
        _fund(firstTask, 4 ether);
        _fund(secondTask, 4 ether);
        TaskEscrow.SelectionProof memory first = _proof(firstTask, 4 ether, 1 ether, 1 ether);
        TaskEscrow.SelectionProof memory second = _proof(secondTask, 4 ether, 1 ether, 1 ether);
        second.assignmentId = first.assignmentId;
        bytes memory firstSignature = _sign(first);
        bytes memory duplicateAssignmentSignature = _sign(second);

        vm.prank(publisher);
        escrow.selectAgent(first, firstSignature);
        vm.expectRevert(TaskEscrow.AlreadyExists.selector);
        vm.prank(publisher);
        escrow.selectAgent(second, duplicateAssignmentSignature);

        second.assignmentId = keccak256("new-assignment");
        second.allocationId = first.allocationId;
        bytes memory duplicateCreditSignature = _sign(second);
        vm.expectRevert(TaskEscrow.AlreadyExists.selector);
        vm.prank(publisher);
        escrow.selectAgent(second, duplicateCreditSignature);
    }

    function testOnlyPublisherCanSubmitSelectionAndNetMustBeFunded() public {
        bytes32 taskId = keccak256("task-auth-funding");
        _fund(taskId, 2 ether);
        TaskEscrow.SelectionProof memory proof = _proof(taskId, 4 ether, 1 ether, 1 ether);
        bytes memory signature = _sign(proof);
        vm.expectRevert(TaskEscrow.NotAuthorized.selector);
        escrow.selectAgent(proof, signature);
        vm.expectRevert(TaskEscrow.InvalidAmount.selector);
        vm.prank(publisher);
        escrow.selectAgent(proof, signature);
    }

    function testTamperedOrCrossContractProofIsRejected() public {
        bytes32 taskId = keccak256("task-tamper");
        _fund(taskId, 4 ether);
        TaskEscrow.SelectionProof memory proof = _proof(taskId, 4 ether, 1 ether, 1 ether);
        bytes memory signature = _sign(proof);
        proof.allocationId = keccak256("other-allocation");
        vm.expectRevert(TaskEscrow.InvalidProof.selector);
        vm.prank(publisher);
        escrow.selectAgent(proof, signature);

        TaskEscrow other = new TaskEscrow(address(this), vm.addr(PLATFORM_SIGNER_KEY));
        vm.prank(publisher);
        other.createTask{value: 4 ether}(taskId);
        proof = _proof(taskId, 4 ether, 1 ether, 1 ether);
        bytes memory crossContractSignature = _signFor(escrow, proof);
        vm.expectRevert(TaskEscrow.InvalidProof.selector);
        vm.prank(publisher);
        other.selectAgent(proof, crossContractSignature);
    }

    function testCrossChainProofIsRejected() public {
        bytes32 taskId = keccak256("task-cross-chain");
        _fund(taskId, 4 ether);
        TaskEscrow.SelectionProof memory proof = _proof(taskId, 4 ether, 1 ether, 1 ether);
        bytes memory signature = _sign(proof);
        vm.chainId(block.chainid + 1);
        vm.expectRevert(TaskEscrow.InvalidProof.selector);
        vm.prank(publisher);
        escrow.selectAgent(proof, signature);
    }

    function testEngineEIP712CompatibilityVector() public {
        TaskEscrow implementation = new TaskEscrow(address(this), vm.addr(PLATFORM_SIGNER_KEY));
        address targetAddress = address(0x1234);
        vm.etch(targetAddress, address(implementation).code);
        vm.chainId(31337);
        TaskEscrow.SelectionProof memory proof = TaskEscrow.SelectionProof({
            taskId: 0xbedae5cad5eecfa155d3192c6941c00d911851f21556a31a38a60c1cbca293f1,
            assignmentId: 0xd69215844c2ee2d422c573c76d746f4d965f5b4a866f63506dac4a6b6cc1df77,
            agentController: address(0xBEEF),
            payout: address(0xF00D),
            overviewId: 0xc1fe9e7e025a0379182c095d48a3a3f7389291c6c308cadb1953fe05037da2c9,
            allocationId: 0xd3b6469f1924c93b192e423fe865493aed78031959b97aefb36924dc676e490a,
            quoteHash: 0xb688a91d28d8930ef89c2836af4c2fdb4ea2f766be531fc4de46c026b096e9ab,
            taskSpecHash: 0x2762e23dd783d2a5776d7c8db9a8039f7d935594dff276b23814c09d1dade887,
            matchRevision: 1,
            priceVersion: 2,
            overviewPrice: 10,
            formalGrossPrice: 100,
            overviewCredit: 10,
            policyHash: 0x7731f7532ec30643f013304ed9481b65865db7265e954df611caaccfcf24e34f,
            nonce: 0x0f3a15d18fcb7b9e0a5a50713e425bd6becde533bdb7fee7656696b8f9867842,
            deadline: 1_800_000_000
        });
        TaskEscrow target = TaskEscrow(targetAddress);
        require(
            target.selectionPayloadHash(proof) == 0x6387ee1a037746da76c6c438c017595e0ceff8aa9fcbf2b62cf566f192afbda9,
            "Engine payload hash mismatch"
        );
        require(
            target.selectionProofDigest(proof) == 0x460ced8c044a195a0fbbdde762c0e137023dab936e855837c75052b0aedff752,
            "Engine proof digest mismatch"
        );
    }

    function testExpiredProofAndExcessCreditAreRejected() public {
        bytes32 taskId = keccak256("task-expired");
        _fund(taskId, 4 ether);
        TaskEscrow.SelectionProof memory proof = _proof(taskId, 4 ether, 1 ether, 1 ether);
        proof.deadline = 0;
        bytes memory expiredSignature = _sign(proof);
        vm.expectRevert(TaskEscrow.ProofExpired.selector);
        vm.prank(publisher);
        escrow.selectAgent(proof, expiredSignature);
        proof.deadline = uint64(block.timestamp + 1 days);
        proof.overviewCredit = proof.overviewPrice + 1;
        bytes memory excessCreditSignature = _sign(proof);
        vm.expectRevert(TaskEscrow.InvalidAmount.selector);
        vm.prank(publisher);
        escrow.selectAgent(proof, excessCreditSignature);
    }

    function testWorkNonceUsesCompareAndSwap() public {
        bytes32 taskId = keccak256("task-work-nonce");
        _fund(taskId, 4 ether);
        TaskEscrow.SelectionProof memory proof = _proof(taskId, 4 ether, 1 ether, 1 ether);
        bytes memory signature = _sign(proof);
        vm.prank(publisher);
        escrow.selectAgent(proof, signature);
        vm.prank(publisher);
        escrow.advanceWorkNonce(taskId, 1);
        require(escrow.workNonces(taskId) == 2, "work nonce was not advanced");
        vm.expectRevert(TaskEscrow.InvalidNonce.selector);
        vm.prank(publisher);
        escrow.advanceWorkNonce(taskId, 1);
    }

    function testSelectionRefundCannotReenter() public {
        ReentrantPublisher attacker = new ReentrantPublisher(escrow);
        bytes32 taskId = keccak256("task-reentrancy");
        vm.deal(address(attacker), 8 ether);
        attacker.fund{value: 8 ether}(taskId);
        TaskEscrow.SelectionProof memory proof = _proof(taskId, 6 ether, 2 ether, 2 ether);

        attacker.select(proof, _sign(proof));

        require(attacker.reentryAttempts() == 1, "refund callback was not exercised");
        require(!attacker.reentrySucceeded(), "reentrant selection succeeded");
        require(escrow.assignmentIdOf(taskId) == proof.assignmentId, "assignment was not confirmed once");
        (,, uint256 locked, TaskEscrow.Status status) = escrow.tasks(taskId);
        require(locked == 4 ether && status == TaskEscrow.Status.Assigned, "reentry changed escrow state");
    }

    function testFuzzSelectionConservesValue(uint96 fundedRaw, uint96 grossRaw, uint96 creditRaw) public {
        uint256 gross = uint256(grossRaw) % 50 ether + 1;
        uint256 credit = uint256(creditRaw) % (gross + 1);
        uint256 net = gross - credit;
        uint256 funded = net + (uint256(fundedRaw) % 50 ether);
        if (funded == 0) funded = 1;
        bytes32 taskId = keccak256(abi.encode(fundedRaw, grossRaw, creditRaw));
        vm.deal(publisher, funded);
        _fund(taskId, funded);
        TaskEscrow.SelectionProof memory proof = _proof(taskId, gross, credit, credit);
        bytes memory signature = _sign(proof);
        uint256 publisherBefore = publisher.balance;
        vm.prank(publisher);
        escrow.selectAgent(proof, signature);
        (,, uint256 locked,) = escrow.tasks(taskId);
        require(locked == net && address(escrow).balance == net, "value was not conserved");
        require(publisher.balance == publisherBefore + funded - net, "wrong excess refund");
    }

    function testAcceptanceAccruesIsolatedEarningsAndControllerWithdrawsToBoundPayout() public {
        bytes32 taskId = keccak256("task-acceptance");
        _fund(taskId, 4 ether);
        TaskEscrow.SelectionProof memory proof = _proof(taskId, 4 ether, 1 ether, 1 ether);
        bytes memory signature = _sign(proof);
        vm.prank(publisher);
        escrow.selectAgent(proof, signature);

        uint256 payoutBefore = payout.balance;
        vm.prank(publisher);
        escrow.accept(taskId);

        (,, uint256 locked, TaskEscrow.Status status) = escrow.tasks(taskId);
        require(locked == 0 && status == TaskEscrow.Status.Released, "task was not accepted");
        require(payout.balance == payoutBefore, "acceptance pushed funds externally");
        require(escrow.claimableEarnings(agentController, payout) == 3 ether, "earnings were not isolated");
        require(escrow.totalClaimableEarnings() == 3 ether, "claimable inventory mismatch");
        require(escrow.yieldEligiblePrincipal(taskId) == 0, "terminal principal remained yield eligible");

        vm.expectRevert(TaskEscrow.InvalidState.selector);
        vm.prank(publisher);
        escrow.accept(taskId);
        vm.expectRevert(TaskEscrow.InvalidAmount.selector);
        vm.prank(address(0xBAD));
        escrow.withdrawEarnings(payable(payout), 1 ether);

        vm.prank(agentController);
        escrow.withdrawEarnings(payable(payout), 2 ether);
        require(payout.balance == payoutBefore + 2 ether, "withdrawal missed bound payout");
        require(escrow.claimableEarnings(agentController, payout) == 1 ether, "partial withdrawal mismatch");
    }

    function testAcceptedEarningsCannotBeRefundedDisputedOrClaimedByAnotherAgent() public {
        bytes32 firstTask = keccak256("settled-isolation");
        bytes32 secondTask = keccak256("unrelated-refund");
        _fund(firstTask, 4 ether);
        TaskEscrow.SelectionProof memory firstProof = _proof(firstTask, 4 ether, 0, 0);
        bytes memory firstSignature = _sign(firstProof);
        vm.prank(publisher);
        escrow.selectAgent(firstProof, firstSignature);
        vm.prank(publisher);
        escrow.release(firstTask);

        _fund(secondTask, 2 ether);
        vm.prank(publisher);
        escrow.refund(secondTask);
        require(escrow.claimableEarnings(agentController, payout) == 4 ether, "refund touched settled earnings");

        vm.expectRevert(TaskEscrow.InvalidState.selector);
        vm.prank(publisher);
        escrow.openDispute(firstTask);
        vm.expectRevert(TaskEscrow.InvalidAmount.selector);
        vm.prank(address(0xDEAD));
        escrow.withdrawEarnings(payable(payout), 1 ether);
    }

    function testOnlyAgentControllerCanOpenDisputeAndAgentAwardBecomesEarnings() public {
        bytes32 taskId = keccak256("agent-dispute");
        _fund(taskId, 3 ether);
        TaskEscrow.SelectionProof memory proof = _proof(taskId, 3 ether, 0, 0);
        bytes memory signature = _sign(proof);
        vm.prank(publisher);
        escrow.selectAgent(proof, signature);

        vm.expectRevert(TaskEscrow.NotAuthorized.selector);
        vm.prank(payout);
        escrow.openDispute(taskId);
        vm.prank(agentController);
        escrow.openDispute(taskId);
        escrow.resolveDispute(taskId, payable(payout));

        require(escrow.claimableEarnings(agentController, payout) == 3 ether, "award was not isolated");
        require(payout.balance == 0, "resolver bypassed pull payment");
    }

    function testYieldEligibilityStartsOnlyAfterSelection() public {
        bytes32 taskId = keccak256("yield-eligibility");
        _fund(taskId, 5 ether);
        require(escrow.yieldEligiblePrincipal(taskId) == 0, "unselected funding became eligible");
        TaskEscrow.SelectionProof memory proof = _proof(taskId, 5 ether, 2 ether, 2 ether);
        bytes memory signature = _sign(proof);
        vm.prank(publisher);
        escrow.selectAgent(proof, signature);
        require(escrow.yieldEligiblePrincipal(taskId) == 3 ether, "selected net was not eligible");
        require(escrow.totalYieldEligiblePrincipal() == 3 ether, "yield inventory mismatch");
    }

    function testWithdrawalCannotReenterOrOverdrawOtherEarnings() public {
        ReentrantAgent agent = new ReentrantAgent(escrow);
        bytes32 taskId = keccak256("withdrawal-reentry");
        _fund(taskId, 2 ether);
        TaskEscrow.SelectionProof memory proof = _proof(taskId, 2 ether, 0, 0);
        proof.agentController = address(agent);
        proof.payout = address(agent);
        bytes memory signature = _sign(proof);
        vm.prank(publisher);
        escrow.selectAgent(proof, signature);
        vm.prank(publisher);
        escrow.accept(taskId);

        agent.withdraw(2 ether);
        require(agent.reentryAttempts() == 1 && !agent.reentrySucceeded(), "withdrawal reentered");
        require(escrow.totalClaimableEarnings() == 0, "withdrawal inventory was not conserved");
        require(address(escrow).balance == 0, "withdrawal left unaccounted value");
    }

    function _fund(bytes32 taskId, uint256 amount) private {
        vm.prank(publisher);
        escrow.createTask{value: amount}(taskId);
    }

    function _proof(bytes32 taskId, uint256 gross, uint256 credit, uint256 overviewPrice)
        private
        view
        returns (TaskEscrow.SelectionProof memory)
    {
        return TaskEscrow.SelectionProof({
            taskId: taskId,
            assignmentId: keccak256(abi.encode("assignment", taskId)),
            agentController: agentController,
            payout: payout,
            overviewId: keccak256(abi.encode("overview", taskId)),
            allocationId: keccak256(abi.encode("allocation", taskId)),
            quoteHash: keccak256(abi.encode("quote", taskId)),
            taskSpecHash: keccak256(abi.encode("spec", taskId)),
            matchRevision: 1,
            priceVersion: 1,
            overviewPrice: overviewPrice,
            formalGrossPrice: gross,
            overviewCredit: credit,
            policyHash: keccak256("selection-policy-v1"),
            nonce: keccak256(abi.encode("nonce", taskId)),
            deadline: uint64(block.timestamp + 1 days)
        });
    }

    function _sign(TaskEscrow.SelectionProof memory proof) private returns (bytes memory) {
        return _signFor(escrow, proof);
    }

    function _signFor(TaskEscrow target, TaskEscrow.SelectionProof memory proof) private returns (bytes memory) {
        (uint8 v, bytes32 r, bytes32 s) = vm.sign(PLATFORM_SIGNER_KEY, target.selectionProofDigest(proof));
        return abi.encodePacked(r, s, v);
    }
}
