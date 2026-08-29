// SPDX-License-Identifier: MIT
pragma solidity ^0.8.24;

import {TaskEscrow} from "../src/TaskEscrow.sol";
import {MockUSDC} from "../src/MockUSDC.sol";

interface Vm {
    function addr(uint256 privateKey) external returns (address);
    function chainId(uint256 newChainId) external;
    function deal(address account, uint256 newBalance) external;
    function etch(address target, bytes calldata code) external;
    function expectRevert(bytes4 selector) external;
    function expectRevert() external;
    function prank(address sender) external;
    function sign(uint256 privateKey, bytes32 digest) external returns (uint8 v, bytes32 r, bytes32 s);
    function warp(uint256 newTimestamp) external;
}

contract WrongDecimalsToken {
    function decimals() external pure returns (uint8) {
        return 18;
    }
}

contract TaskEscrowTest {
    Vm private constant vm = Vm(address(uint160(uint256(keccak256("hevm cheat code")))));
    uint256 private constant PLATFORM_SIGNER_KEY = 0xA11CE;
    address private constant publisher = address(0xCAFE);
    address private constant agentController = address(0xBEEF);
    address private constant payout = address(0xF00D);
    TaskEscrow private escrow;
    MockUSDC private token;

    receive() external payable {}

    function setUp() public {
        token = new MockUSDC();
        escrow =
            new TaskEscrow(address(token), address(this), address(this), address(this), vm.addr(PLATFORM_SIGNER_KEY));
        token.mint(publisher, 100 ether);
        vm.prank(publisher);
        token.approve(address(escrow), type(uint256).max);
    }

    function testFrozenLeafAllocationRequiresCompleteStableValueConservingCoverage() public {
        bytes32 taskId = _fund(keccak256("leaf-dispute"), 4 ether);
        TaskEscrow.SelectionProof memory proof = _proof(taskId, 4 ether, 1 ether, 1 ether);
        bytes memory signature = _sign(proof);
        vm.prank(publisher);
        escrow.selectAgent(proof, signature);
        TaskEscrow.FrozenLeaf[] memory leaves = new TaskEscrow.FrozenLeaf[](2);
        leaves[0] = TaskEscrow.FrozenLeaf(0, publisher, 3 ether, 0);
        leaves[1] = TaskEscrow.FrozenLeaf(1, payout, 3 ether, 1);
        vm.prank(publisher);
        escrow.freezeDispute(taskId, leaves, address(this), 0.5 ether);

        TaskEscrow.DisputeAllocation[] memory incomplete = new TaskEscrow.DisputeAllocation[](1);
        incomplete[0] = TaskEscrow.DisputeAllocation(0, publisher, 2 ether);
        vm.expectRevert(TaskEscrow.InvalidState.selector);
        escrow.finalizeDisputeAllocation(taskId, incomplete, 0.5 ether);
        vm.warp(block.timestamp + 1 days);
        vm.expectRevert(TaskEscrow.InvalidProof.selector);
        escrow.finalizeDisputeAllocation(taskId, incomplete, 0.5 ether);

        TaskEscrow.DisputeAllocation[] memory allocation = new TaskEscrow.DisputeAllocation[](2);
        allocation[0] = TaskEscrow.DisputeAllocation(0, publisher, 1 ether);
        allocation[1] = TaskEscrow.DisputeAllocation(1, payout, 1.5 ether);
        uint256 publisherBefore = token.balanceOf(publisher);
        escrow.finalizeDisputeAllocation(taskId, allocation, 0.5 ether);
        require(token.balanceOf(publisher) == publisherBefore + 1 ether, "publisher leaf not paid");
        require(escrow.claimableEarnings(agentController, payout) == 1.5 ether, "agent leaf not isolated");
        require(
            token.balanceOf(address(escrow)) == escrow.totalClaimableEarnings(),
            "escrow balance did not retain isolated Agent earnings"
        );
    }

    function testFrozenLeafAllocationRejectsOwnerReplacementAndReplay() public {
        bytes32 taskId = _fund(keccak256("leaf-owner"), 3 ether);
        TaskEscrow.SelectionProof memory proof = _proof(taskId, 3 ether, 0, 0);
        bytes memory signature = _sign(proof);
        vm.prank(publisher);
        escrow.selectAgent(proof, signature);
        TaskEscrow.FrozenLeaf[] memory leaves = new TaskEscrow.FrozenLeaf[](2);
        leaves[0] = TaskEscrow.FrozenLeaf(0, publisher, 3 ether, 0);
        leaves[1] = TaskEscrow.FrozenLeaf(1, payout, 3 ether, 1);
        vm.prank(agentController);
        escrow.freezeDispute(taskId, leaves, address(this), 0);
        TaskEscrow.DisputeAllocation[] memory allocation = new TaskEscrow.DisputeAllocation[](2);
        allocation[0] = TaskEscrow.DisputeAllocation(0, address(0xBAD), 1 ether);
        allocation[1] = TaskEscrow.DisputeAllocation(1, payout, 2 ether);
        vm.warp(block.timestamp + 1 days);
        vm.expectRevert(TaskEscrow.InvalidProof.selector);
        escrow.finalizeDisputeAllocation(taskId, allocation, 0);
        allocation[0] = TaskEscrow.DisputeAllocation(0, publisher, 1 ether);
        escrow.finalizeDisputeAllocation(taskId, allocation, 0);
        vm.expectRevert(TaskEscrow.InvalidState.selector);
        escrow.finalizeDisputeAllocation(taskId, allocation, 0);
    }

    function testSelectionAppliesCreditLocksNetRefundsExcessAndStartsV1() public {
        bytes32 taskId = _fund(keccak256("task-selection"), 8 ether);
        TaskEscrow.SelectionProof memory proof = _proof(taskId, 6 ether, 2 ether, 2 ether);
        bytes memory signature = _sign(proof);
        uint256 publisherBefore = token.balanceOf(publisher);
        vm.prank(publisher);
        escrow.selectAgent(proof, signature);

        (address storedPublisher, address storedAgent, uint256 amount, TaskEscrow.Status status) = escrow.tasks(taskId);
        require(storedPublisher == publisher && storedAgent == payout, "identity binding changed");
        require(amount == 4 ether && status == TaskEscrow.Status.Assigned, "wrong formal lock");
        require(token.balanceOf(publisher) == publisherBefore + 4 ether, "excess was not refunded");
        require(token.balanceOf(address(escrow)) == 4 ether, "escrow value was not conserved");
        require(escrow.workNonces(taskId) == 1, "V1 work nonce was not initialized");
        require(escrow.assignmentIdOf(taskId) == proof.assignmentId, "assignment id changed");
    }

    function testSelectionReplayProducesOnlyOneAssignment() public {
        bytes32 taskId = _fund(keccak256("task-replay"), 4 ether);
        TaskEscrow.SelectionProof memory proof = _proof(taskId, 4 ether, 1 ether, 1 ether);
        bytes memory signature = _sign(proof);
        vm.prank(publisher);
        escrow.selectAgent(proof, signature);
        vm.expectRevert(TaskEscrow.InvalidState.selector);
        vm.prank(publisher);
        escrow.selectAgent(proof, signature);
    }

    function testNonceCannotBeReusedAcrossTasks() public {
        bytes32 firstTask = _fund(keccak256("task-first"), 4 ether);
        bytes32 secondTask = _fund(keccak256("task-second"), 4 ether);
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
        bytes32 firstTask = _fund(keccak256("task-binding-first"), 4 ether);
        bytes32 secondTask = _fund(keccak256("task-binding-second"), 4 ether);
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
        bytes32 taskId = _fund(keccak256("task-auth-funding"), 2 ether);
        TaskEscrow.SelectionProof memory proof = _proof(taskId, 4 ether, 1 ether, 1 ether);
        bytes memory signature = _sign(proof);
        vm.expectRevert(TaskEscrow.NotAuthorized.selector);
        escrow.selectAgent(proof, signature);
        vm.expectRevert(TaskEscrow.InvalidAmount.selector);
        vm.prank(publisher);
        escrow.selectAgent(proof, signature);
    }

    function testTamperedOrCrossContractProofIsRejected() public {
        bytes32 taskId = _fund(keccak256("task-tamper"), 4 ether);
        TaskEscrow.SelectionProof memory proof = _proof(taskId, 4 ether, 1 ether, 1 ether);
        bytes memory signature = _sign(proof);
        proof.allocationId = keccak256("other-allocation");
        vm.expectRevert(TaskEscrow.InvalidProof.selector);
        vm.prank(publisher);
        escrow.selectAgent(proof, signature);

        TaskEscrow other =
            new TaskEscrow(address(token), address(this), address(this), address(this), vm.addr(PLATFORM_SIGNER_KEY));
        vm.prank(publisher);
        token.approve(address(other), type(uint256).max);
        bytes32 otherTaskId;
        vm.prank(publisher);
        otherTaskId = other.createTask(
            keccak256("other-task"), keccak256("other-spec"), uint64(block.timestamp + 1 days), 4 ether
        );
        proof = _proof(otherTaskId, 4 ether, 1 ether, 1 ether);
        bytes memory crossContractSignature = _signFor(escrow, proof);
        vm.expectRevert(TaskEscrow.InvalidProof.selector);
        vm.prank(publisher);
        other.selectAgent(proof, crossContractSignature);
    }

    function testCrossChainProofIsRejected() public {
        bytes32 taskId = _fund(keccak256("task-cross-chain"), 4 ether);
        TaskEscrow.SelectionProof memory proof = _proof(taskId, 4 ether, 1 ether, 1 ether);
        bytes memory signature = _sign(proof);
        vm.chainId(block.chainid + 1);
        vm.expectRevert(TaskEscrow.InvalidProof.selector);
        vm.prank(publisher);
        escrow.selectAgent(proof, signature);
    }

    function testEngineEIP712CompatibilityVector() public {
        TaskEscrow implementation =
            new TaskEscrow(address(token), address(this), address(this), address(this), vm.addr(PLATFORM_SIGNER_KEY));
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
        bytes32 taskId = _fund(keccak256("task-expired"), 4 ether);
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
        bytes32 taskId = _fund(keccak256("task-work-nonce"), 4 ether);
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

    function testSelectionRefundUsesBoundERC20AndPreservesAssignment() public {
        bytes32 taskId = _fund(keccak256("task-refund"), 8 ether);
        TaskEscrow.SelectionProof memory proof = _proof(taskId, 6 ether, 2 ether, 2 ether);
        bytes memory signature = _sign(proof);
        vm.prank(publisher);
        escrow.selectAgent(proof, signature);
        require(escrow.assignmentIdOf(taskId) == proof.assignmentId, "assignment was not confirmed once");
        (,, uint256 locked, TaskEscrow.Status status) = escrow.tasks(taskId);
        require(locked == 4 ether && status == TaskEscrow.Status.Assigned, "refund changed escrow state");
        require(token.balanceOf(address(escrow)) == 4 ether, "refund did not preserve net escrow");
    }

    function testFuzzSelectionConservesValue(uint96 fundedRaw, uint96 grossRaw, uint96 creditRaw) public {
        uint256 gross = uint256(grossRaw) % 50 ether + 1;
        uint256 credit = uint256(creditRaw) % (gross + 1);
        uint256 net = gross - credit;
        uint256 funded = net + (uint256(fundedRaw) % 50 ether);
        if (funded == 0) funded = 1;
        token.mint(publisher, funded);
        bytes32 taskId = _fund(keccak256(abi.encode(fundedRaw, grossRaw, creditRaw)), funded);
        TaskEscrow.SelectionProof memory proof = _proof(taskId, gross, credit, credit);
        bytes memory signature = _sign(proof);
        uint256 publisherBefore = token.balanceOf(publisher);
        vm.prank(publisher);
        escrow.selectAgent(proof, signature);
        (,, uint256 locked,) = escrow.tasks(taskId);
        require(locked == net && token.balanceOf(address(escrow)) == net, "value was not conserved");
        require(token.balanceOf(publisher) == publisherBefore + funded - net, "wrong excess refund");
    }

    function testAcceptanceAccruesIsolatedEarningsAndControllerWithdrawsToBoundPayout() public {
        bytes32 taskId = _fund(keccak256("task-acceptance"), 4 ether);
        TaskEscrow.SelectionProof memory proof = _proof(taskId, 4 ether, 1 ether, 1 ether);
        bytes memory signature = _sign(proof);
        vm.prank(publisher);
        escrow.selectAgent(proof, signature);

        uint256 payoutBefore = token.balanceOf(payout);
        vm.prank(publisher);
        escrow.accept(taskId);

        (,, uint256 locked, TaskEscrow.Status status) = escrow.tasks(taskId);
        require(locked == 0 && status == TaskEscrow.Status.Released, "task was not accepted");
        require(token.balanceOf(payout) == payoutBefore, "acceptance pushed funds externally");
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
        require(token.balanceOf(payout) == payoutBefore + 2 ether, "withdrawal missed bound payout");
        require(escrow.claimableEarnings(agentController, payout) == 1 ether, "partial withdrawal mismatch");
    }

    function testAcceptedEarningsCannotBeRefundedDisputedOrClaimedByAnotherAgent() public {
        bytes32 firstTask = _fund(keccak256("settled-isolation"), 4 ether);
        TaskEscrow.SelectionProof memory firstProof = _proof(firstTask, 4 ether, 0, 0);
        bytes memory firstSignature = _sign(firstProof);
        vm.prank(publisher);
        escrow.selectAgent(firstProof, firstSignature);
        vm.prank(publisher);
        escrow.release(firstTask);

        bytes32 secondTask = _fund(keccak256("unrelated-refund"), 2 ether);
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
        bytes32 taskId = _fund(keccak256("agent-dispute"), 3 ether);
        TaskEscrow.SelectionProof memory proof = _proof(taskId, 3 ether, 0, 0);
        bytes memory signature = _sign(proof);
        vm.prank(publisher);
        escrow.selectAgent(proof, signature);

        TaskEscrow.FrozenLeaf[] memory leaves = new TaskEscrow.FrozenLeaf[](2);
        leaves[0] = TaskEscrow.FrozenLeaf(0, publisher, 3 ether, 0);
        leaves[1] = TaskEscrow.FrozenLeaf(1, payout, 3 ether, 1);
        vm.expectRevert(TaskEscrow.NotAuthorized.selector);
        vm.prank(payout);
        escrow.freezeDispute(taskId, leaves, address(this), 0);
        vm.prank(agentController);
        escrow.freezeDispute(taskId, leaves, address(this), 0);
        TaskEscrow.DisputeAllocation[] memory allocation = new TaskEscrow.DisputeAllocation[](2);
        allocation[0] = TaskEscrow.DisputeAllocation(0, publisher, 0);
        allocation[1] = TaskEscrow.DisputeAllocation(1, payout, 3 ether);
        vm.warp(block.timestamp + 1 days);
        escrow.finalizeDisputeAllocation(taskId, allocation, 0);

        require(escrow.claimableEarnings(agentController, payout) == 3 ether, "award was not isolated");
        require(token.balanceOf(payout) == 0, "resolver bypassed pull payment");
    }

    function testDeprecatedSingleRecipientDisputePathFailsClosed() public {
        bytes32 taskId = _fund(keccak256("deprecated-dispute"), 1 ether);
        TaskEscrow.SelectionProof memory proof = _proof(taskId, 1 ether, 0, 0);
        bytes memory signature = _sign(proof);
        vm.prank(publisher);
        escrow.selectAgent(proof, signature);
        vm.expectRevert(TaskEscrow.InvalidState.selector);
        vm.prank(publisher);
        escrow.openDispute(taskId);
        vm.expectRevert(TaskEscrow.InvalidState.selector);
        escrow.resolveDispute(taskId, payout);
    }

    function testYieldEligibilityStartsOnlyAfterSelection() public {
        bytes32 taskId = _fund(keccak256("yield-eligibility"), 5 ether);
        require(escrow.yieldEligiblePrincipal(taskId) == 0, "unselected funding became eligible");
        TaskEscrow.SelectionProof memory proof = _proof(taskId, 5 ether, 2 ether, 2 ether);
        bytes memory signature = _sign(proof);
        vm.prank(publisher);
        escrow.selectAgent(proof, signature);
        require(escrow.yieldEligiblePrincipal(taskId) == 3 ether, "selected net was not eligible");
        require(escrow.totalYieldEligiblePrincipal() == 3 ether, "yield inventory mismatch");
    }

    function testWithdrawalCannotOverdrawOtherEarnings() public {
        bytes32 taskId = _fund(keccak256("withdrawal-overdraw"), 2 ether);
        TaskEscrow.SelectionProof memory proof = _proof(taskId, 2 ether, 0, 0);
        bytes memory signature = _sign(proof);
        vm.prank(publisher);
        escrow.selectAgent(proof, signature);
        vm.prank(publisher);
        escrow.accept(taskId);

        vm.prank(agentController);
        escrow.withdrawEarnings(payout, 2 ether);
        vm.expectRevert(TaskEscrow.InvalidAmount.selector);
        vm.prank(agentController);
        escrow.withdrawEarnings(payout, 1);
        require(escrow.totalClaimableEarnings() == 0, "withdrawal inventory was not conserved");
        require(token.balanceOf(address(escrow)) == 0, "withdrawal left unaccounted value");
    }

    function testConstructorRejectsWrongDecimalsAndZeroAddresses() public {
        WrongDecimalsToken wrongDecimals = new WrongDecimalsToken();
        vm.expectRevert(TaskEscrow.InvalidAsset.selector);
        new TaskEscrow(
            address(wrongDecimals), address(this), address(this), address(this), vm.addr(PLATFORM_SIGNER_KEY)
        );

        vm.expectRevert(TaskEscrow.InvalidAddress.selector);
        new TaskEscrow(address(0), address(this), address(this), address(this), vm.addr(PLATFORM_SIGNER_KEY));
        vm.expectRevert();
        new TaskEscrow(address(token), address(0), address(this), address(this), vm.addr(PLATFORM_SIGNER_KEY));
        vm.expectRevert(TaskEscrow.InvalidAddress.selector);
        new TaskEscrow(address(token), address(this), address(0), address(this), vm.addr(PLATFORM_SIGNER_KEY));
        vm.expectRevert(TaskEscrow.InvalidAddress.selector);
        new TaskEscrow(address(token), address(this), address(this), address(0), vm.addr(PLATFORM_SIGNER_KEY));
        vm.expectRevert(TaskEscrow.InvalidAddress.selector);
        new TaskEscrow(address(token), address(this), address(this), address(this), address(0));
    }

    function testTaskIdUsesEveryFrozenDomainFieldAndRejectsDuplicate() public {
        bytes32 platformTaskKey = keccak256("task-id-key");
        bytes32 taskSpecHash = keccak256("task-id-spec");
        uint64 deadline = uint64(block.timestamp + 1 days);
        uint256 budget = 1_000_000;
        bytes32 taskId = escrow.deriveTaskId(publisher, platformTaskKey, taskSpecHash, deadline, budget);
        bytes32 expected = keccak256(
            abi.encode(
                "agent-platform-task-v3",
                block.chainid,
                address(escrow),
                address(token),
                publisher,
                platformTaskKey,
                taskSpecHash,
                deadline,
                budget
            )
        );
        require(taskId == expected, "task id domain changed");
        require(
            escrow.deriveTaskId(address(0xBAD), platformTaskKey, taskSpecHash, deadline, budget) != taskId,
            "publisher omitted"
        );
        require(
            escrow.deriveTaskId(publisher, keccak256("other-key"), taskSpecHash, deadline, budget) != taskId,
            "key omitted"
        );
        require(
            escrow.deriveTaskId(publisher, platformTaskKey, keccak256("other-spec"), deadline, budget) != taskId,
            "spec omitted"
        );
        require(
            escrow.deriveTaskId(publisher, platformTaskKey, taskSpecHash, deadline + 1, budget) != taskId,
            "deadline omitted"
        );
        require(
            escrow.deriveTaskId(publisher, platformTaskKey, taskSpecHash, deadline, budget + 1) != taskId,
            "budget omitted"
        );

        TaskEscrow other =
            new TaskEscrow(address(token), address(this), address(this), address(this), vm.addr(PLATFORM_SIGNER_KEY));
        require(
            other.deriveTaskId(publisher, platformTaskKey, taskSpecHash, deadline, budget) != taskId, "contract omitted"
        );
        vm.chainId(block.chainid + 1);
        require(
            escrow.deriveTaskId(publisher, platformTaskKey, taskSpecHash, deadline, budget) != taskId, "chain omitted"
        );

        vm.prank(publisher);
        bytes32 created = escrow.createTask(platformTaskKey, taskSpecHash, deadline, budget);
        require(created != bytes32(0), "task was not created");
        vm.expectRevert(TaskEscrow.AlreadyExists.selector);
        vm.prank(publisher);
        escrow.createTask(platformTaskKey, taskSpecHash, deadline, budget);
    }

    function testGuardianPausesButOnlyGovernanceUnpausesAndRotates() public {
        address guardian = address(0xABCD);
        address nextSigner = address(0x1234);
        address nextResolver = address(0x5678);
        TaskEscrow governed =
            new TaskEscrow(address(token), address(this), guardian, address(this), vm.addr(PLATFORM_SIGNER_KEY));

        vm.prank(guardian);
        governed.pause();
        require(governed.paused(), "guardian did not pause");
        vm.expectRevert();
        vm.prank(guardian);
        governed.unpause();
        governed.unpause();
        governed.setPlatformProofSigner(nextSigner);
        governed.setDisputeResolver(nextResolver);
        require(governed.platformProofSigner() == nextSigner, "signer did not rotate");
        require(governed.disputeResolver() == nextResolver, "resolver did not rotate");

        vm.expectRevert(TaskEscrow.InvalidAddress.selector);
        governed.setPlatformProofSigner(address(0));
        vm.expectRevert(TaskEscrow.InvalidAddress.selector);
        governed.setDisputeResolver(address(0));
    }

    function testPauseStopsOnlyNewRiskIncreasingTransitions() public {
        bytes32 fundedTask = _fund(keccak256("paused-funded"), 1 ether);
        bytes32 assignedTask = _fund(keccak256("paused-assigned"), 2 ether);
        TaskEscrow.SelectionProof memory assignedProof = _proof(assignedTask, 2 ether, 0, 0);
        bytes memory assignedSignature = _sign(assignedProof);
        vm.prank(publisher);
        escrow.selectAgent(assignedProof, assignedSignature);
        TaskEscrow.SelectionProof memory fundedProof = _proof(fundedTask, 1 ether, 0, 0);
        bytes memory fundedSignature = _sign(fundedProof);
        TaskEscrow.FrozenLeaf[] memory leaves = _leaves(assignedTask);

        escrow.pause();
        vm.expectRevert();
        vm.prank(publisher);
        escrow.createTask(
            keccak256("paused-create"), keccak256("paused-spec"), uint64(block.timestamp + 1 days), 1 ether
        );
        vm.expectRevert();
        vm.prank(publisher);
        escrow.selectAgent(fundedProof, fundedSignature);
        vm.expectRevert();
        vm.prank(publisher);
        escrow.advanceWorkNonce(assignedTask, 1);
        vm.expectRevert();
        vm.prank(publisher);
        escrow.freezeDispute(assignedTask, leaves, address(this), 0);
    }

    function testPauseKeepsRefundWithdrawalAndExistingDisputeFinalizationOpen() public {
        bytes32 refundableTask = _fund(keccak256("paused-refund"), 1 ether);

        bytes32 acceptedTask = _fund(keccak256("paused-withdraw"), 1 ether);
        TaskEscrow.SelectionProof memory acceptedProof = _proof(acceptedTask, 1 ether, 0, 0);
        bytes memory acceptedSignature = _sign(acceptedProof);
        vm.prank(publisher);
        escrow.selectAgent(acceptedProof, acceptedSignature);
        vm.prank(publisher);
        escrow.accept(acceptedTask);

        bytes32 disputedTask = _fund(keccak256("paused-finalize"), 2 ether);
        TaskEscrow.SelectionProof memory disputedProof = _proof(disputedTask, 2 ether, 0, 0);
        bytes memory disputedSignature = _sign(disputedProof);
        vm.prank(publisher);
        escrow.selectAgent(disputedProof, disputedSignature);
        TaskEscrow.FrozenLeaf[] memory leaves = _leaves(disputedTask);
        vm.prank(publisher);
        escrow.freezeDispute(disputedTask, leaves, address(this), 0);

        escrow.pause();
        vm.prank(publisher);
        escrow.refund(refundableTask);
        vm.prank(agentController);
        escrow.withdrawEarnings(payout, 1 ether);

        TaskEscrow.DisputeAllocation[] memory allocations = new TaskEscrow.DisputeAllocation[](2);
        allocations[0] = TaskEscrow.DisputeAllocation(0, publisher, 1 ether);
        allocations[1] = TaskEscrow.DisputeAllocation(1, payout, 1 ether);
        vm.warp(block.timestamp + 1 days);
        escrow.finalizeDisputeAllocation(disputedTask, allocations, 0);
        require(escrow.claimableEarnings(agentController, payout) == 1 ether, "paused finalization failed");
    }

    function _fund(bytes32 platformTaskKey, uint256 amount) private returns (bytes32 taskId) {
        bytes32 taskSpecHash = keccak256(abi.encode("funding-spec", platformTaskKey));
        uint64 fundingDeadline = uint64(block.timestamp + 1 days);
        vm.prank(publisher);
        taskId = escrow.createTask(platformTaskKey, taskSpecHash, fundingDeadline, amount);
    }

    function _leaves(bytes32 taskId) private view returns (TaskEscrow.FrozenLeaf[] memory leaves) {
        (address taskPublisher,, uint256 amount,) = escrow.tasks(taskId);
        leaves = new TaskEscrow.FrozenLeaf[](2);
        leaves[0] = TaskEscrow.FrozenLeaf(0, taskPublisher, amount, 0);
        leaves[1] = TaskEscrow.FrozenLeaf(1, payout, amount, 1);
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
