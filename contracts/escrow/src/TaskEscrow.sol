// SPDX-License-Identifier: MIT
pragma solidity ^0.8.24;

/// @notice Native-asset escrow used by the local MVP deployment. Production asset,
/// precision and signer governance remain deployment decisions.
contract TaskEscrow {
    enum Status {
        None,
        Funded,
        Assigned,
        Released,
        Refunded,
        Disputed
    }

    struct Task {
        address publisher;
        address agent;
        uint256 amount;
        Status status;
    }

    /// @dev Every field is covered by the platform EIP-712 proof. The publisher
    /// authorizes the same values by submitting the transaction directly.
    struct SelectionProof {
        bytes32 taskId;
        bytes32 assignmentId;
        address agentController;
        address payout;
        bytes32 overviewId;
        bytes32 allocationId;
        bytes32 quoteHash;
        bytes32 taskSpecHash;
        uint64 matchRevision;
        uint64 priceVersion;
        uint256 overviewPrice;
        uint256 formalGrossPrice;
        uint256 overviewCredit;
        bytes32 policyHash;
        bytes32 nonce;
        uint64 deadline;
    }

    struct Assignment {
        bytes32 id;
        address agentController;
        address payout;
        bytes32 overviewId;
        bytes32 allocationId;
        bytes32 quoteHash;
        bytes32 taskSpecHash;
        uint64 matchRevision;
        uint64 priceVersion;
        uint256 formalGrossPrice;
        uint256 overviewCredit;
        uint256 formalPayable;
        bytes32 policyHash;
    }

    error AlreadyExists();
    error InvalidAddress();
    error InvalidAmount();
    error InvalidNonce();
    error InvalidProof();
    error InvalidState();
    error NotAuthorized();
    error ProofExpired();
    error TransferFailed();

    event TaskCreated(bytes32 indexed taskId, address indexed publisher, uint256 amount);
    event SelectionConfirmed(
        bytes32 indexed taskId,
        bytes32 indexed assignmentId,
        address indexed agentController,
        address payout,
        bytes32 overviewId,
        bytes32 allocationId,
        uint256 formalGrossPrice,
        uint256 overviewCredit,
        uint256 formalPayable,
        uint256 excessRefunded,
        uint256 workNonce
    );
    event WorkNonceAdvanced(bytes32 indexed taskId, bytes32 indexed assignmentId, uint256 workNonce);
    event FundsReleased(bytes32 indexed taskId, address indexed agent, uint256 amount);
    event FundsRefunded(bytes32 indexed taskId, address indexed publisher, uint256 amount);
    event EarningsAccrued(
        bytes32 indexed taskId,
        bytes32 indexed assignmentId,
        address indexed agentController,
        address payout,
        uint256 amount
    );
    event EarningsWithdrawn(address indexed agentController, address indexed payout, uint256 amount);
    event YieldEligibilityChanged(bytes32 indexed taskId, uint256 amount, bool eligible);
    event DisputeOpened(bytes32 indexed taskId, address indexed openedBy);
    event DisputeResolved(bytes32 indexed taskId, address indexed recipient, uint256 amount);

    bytes32 public constant SELECTION_PROOF_TYPEHASH = keccak256("SelectionProof(bytes32 payloadHash)");
    bytes32 private constant EIP712_DOMAIN_TYPEHASH =
        keccak256("EIP712Domain(string name,string version,uint256 chainId,address verifyingContract)");
    bytes32 private constant NAME_HASH = keccak256("AgentTaskEscrow");
    bytes32 private constant VERSION_HASH = keccak256("1");
    uint256 private constant SECP256K1_HALF_ORDER = 0x7fffffffffffffffffffffffffffffff5d576e7357a4501ddfe92f46681b20a0;

    address public immutable disputeResolver;
    address public immutable platformProofSigner;
    mapping(bytes32 taskId => Task task) public tasks;
    mapping(bytes32 taskId => Assignment assignment) public assignments;
    mapping(bytes32 taskId => uint256 nonce) public workNonces;
    mapping(bytes32 nonce => bool used) public usedSelectionNonces;
    mapping(bytes32 assignmentId => bool used) public usedAssignmentIds;
    mapping(bytes32 allocationId => bool used) public usedOverviewCredits;
    mapping(address agentController => mapping(address payout => uint256 amount)) public claimableEarnings;
    mapping(bytes32 taskId => uint256 amount) public yieldEligiblePrincipal;
    uint256 public totalClaimableEarnings;
    uint256 public totalYieldEligiblePrincipal;

    uint256 private locked = 1;

    constructor(address resolver, address proofSigner) {
        if (resolver == address(0) || proofSigner == address(0)) revert InvalidAddress();
        disputeResolver = resolver;
        platformProofSigner = proofSigner;
    }

    modifier nonReentrant() {
        if (locked != 1) revert InvalidState();
        locked = 2;
        _;
        locked = 1;
    }

    function createTask(bytes32 taskId) external payable {
        if (taskId == bytes32(0)) revert InvalidState();
        if (msg.value == 0) revert InvalidAmount();
        if (tasks[taskId].status != Status.None) revert AlreadyExists();
        tasks[taskId] = Task(msg.sender, address(0), msg.value, Status.Funded);
        emit TaskCreated(taskId, msg.sender, msg.value);
    }

    /// @notice Atomically consumes the signed selection proof, creates the only
    /// assignment, applies overview credit, locks formal net, refunds surplus and starts V1.
    function selectAgent(SelectionProof calldata proof, bytes calldata signature) external nonReentrant {
        Task storage task = tasks[proof.taskId];
        if (msg.sender != task.publisher) revert NotAuthorized();
        if (task.status != Status.Funded) revert InvalidState();
        if (
            proof.taskId == bytes32(0) || proof.assignmentId == bytes32(0) || proof.agentController == address(0)
                || proof.payout == address(0) || proof.overviewId == bytes32(0) || proof.allocationId == bytes32(0)
                || proof.quoteHash == bytes32(0) || proof.taskSpecHash == bytes32(0) || proof.matchRevision == 0
                || proof.priceVersion == 0 || proof.policyHash == bytes32(0)
        ) revert InvalidProof();
        if (proof.deadline < block.timestamp) revert ProofExpired();
        if (proof.nonce == bytes32(0) || usedSelectionNonces[proof.nonce]) revert InvalidNonce();
        if (
            assignments[proof.taskId].id != bytes32(0) || usedAssignmentIds[proof.assignmentId]
                || usedOverviewCredits[proof.allocationId]
        ) revert AlreadyExists();
        if (
            proof.formalGrossPrice == 0 || proof.overviewCredit > proof.overviewPrice
                || proof.overviewCredit > proof.formalGrossPrice
        ) revert InvalidAmount();

        uint256 formalPayable = proof.formalGrossPrice - proof.overviewCredit;
        if (task.amount < formalPayable) revert InvalidAmount();
        if (_recover(selectionProofDigest(proof), signature) != platformProofSigner) revert InvalidProof();

        uint256 excess = task.amount - formalPayable;
        usedSelectionNonces[proof.nonce] = true;
        usedAssignmentIds[proof.assignmentId] = true;
        usedOverviewCredits[proof.allocationId] = true;
        task.agent = proof.payout;
        task.amount = formalPayable;
        task.status = Status.Assigned;
        assignments[proof.taskId] = Assignment({
            id: proof.assignmentId,
            agentController: proof.agentController,
            payout: proof.payout,
            overviewId: proof.overviewId,
            allocationId: proof.allocationId,
            quoteHash: proof.quoteHash,
            taskSpecHash: proof.taskSpecHash,
            matchRevision: proof.matchRevision,
            priceVersion: proof.priceVersion,
            formalGrossPrice: proof.formalGrossPrice,
            overviewCredit: proof.overviewCredit,
            formalPayable: formalPayable,
            policyHash: proof.policyHash
        });
        workNonces[proof.taskId] = 1;
        yieldEligiblePrincipal[proof.taskId] = formalPayable;
        totalYieldEligiblePrincipal += formalPayable;

        if (excess != 0) {
            (bool refunded,) = payable(task.publisher).call{value: excess}("");
            if (!refunded) revert TransferFailed();
        }

        emit SelectionConfirmed(
            proof.taskId,
            proof.assignmentId,
            proof.agentController,
            proof.payout,
            proof.overviewId,
            proof.allocationId,
            proof.formalGrossPrice,
            proof.overviewCredit,
            formalPayable,
            excess,
            1
        );
        emit YieldEligibilityChanged(proof.taskId, formalPayable, true);
    }

    /// @notice Compare-and-swap prevents concurrent/replayed work authorizations from skipping a nonce.
    function advanceWorkNonce(bytes32 taskId, uint256 expectedCurrentNonce) external {
        Task storage task = tasks[taskId];
        if (msg.sender != task.publisher) revert NotAuthorized();
        if (task.status != Status.Assigned) revert InvalidState();
        if (workNonces[taskId] != expectedCurrentNonce || expectedCurrentNonce == 0) revert InvalidNonce();
        uint256 nextNonce = expectedCurrentNonce + 1;
        workNonces[taskId] = nextNonce;
        emit WorkNonceAdvanced(taskId, assignments[taskId].id, nextNonce);
    }

    function selectionPayloadHash(SelectionProof calldata proof) public pure returns (bytes32) {
        return keccak256(abi.encode(proof));
    }

    function selectionProofDigest(SelectionProof calldata proof) public view returns (bytes32) {
        bytes32 structHash = keccak256(abi.encode(SELECTION_PROOF_TYPEHASH, selectionPayloadHash(proof)));
        return keccak256(abi.encodePacked("\x19\x01", domainSeparator(), structHash));
    }

    function domainSeparator() public view returns (bytes32) {
        return keccak256(abi.encode(EIP712_DOMAIN_TYPEHASH, NAME_HASH, VERSION_HASH, block.chainid, address(this)));
    }

    function assignmentIdOf(bytes32 taskId) external view returns (bytes32) {
        return assignments[taskId].id;
    }

    /// @notice Acceptance makes the whole V1 formal package payable. Unused
    /// work rounds are intentionally not refunded. Earnings remain isolated
    /// until the bound Agent controller withdraws them to its bound payout.
    function accept(bytes32 taskId) external nonReentrant {
        _accept(taskId);
    }

    /// @notice Backwards-compatible acceptance entrypoint.
    function release(bytes32 taskId) external nonReentrant {
        _accept(taskId);
    }

    function _accept(bytes32 taskId) private {
        Task storage task = tasks[taskId];
        if (msg.sender != task.publisher) revert NotAuthorized();
        if (task.status != Status.Assigned) revert InvalidState();
        uint256 amount = task.amount;
        Assignment storage assignment = assignments[taskId];
        task.status = Status.Released;
        task.amount = 0;
        _endYieldEligibility(taskId);
        claimableEarnings[assignment.agentController][assignment.payout] += amount;
        totalClaimableEarnings += amount;
        emit FundsReleased(taskId, assignment.payout, amount);
        emit EarningsAccrued(taskId, assignment.id, assignment.agentController, assignment.payout, amount);
    }

    function withdrawEarnings(address payable payout, uint256 amount) external nonReentrant {
        if (payout == address(0)) revert InvalidAddress();
        if (amount == 0) revert InvalidAmount();
        uint256 available = claimableEarnings[msg.sender][payout];
        if (amount > available) revert InvalidAmount();
        claimableEarnings[msg.sender][payout] = available - amount;
        totalClaimableEarnings -= amount;
        (bool sent,) = payout.call{value: amount}("");
        if (!sent) revert TransferFailed();
        emit EarningsWithdrawn(msg.sender, payout, amount);
    }

    function refund(bytes32 taskId) external nonReentrant {
        Task storage task = tasks[taskId];
        if (msg.sender != task.publisher) revert NotAuthorized();
        if (task.status != Status.Funded) revert InvalidState();
        uint256 amount = task.amount;
        task.status = Status.Refunded;
        task.amount = 0;
        _endYieldEligibility(taskId);
        (bool sent,) = payable(task.publisher).call{value: amount}("");
        if (!sent) revert TransferFailed();
        emit FundsRefunded(taskId, task.publisher, amount);
    }

    function openDispute(bytes32 taskId) external {
        Task storage task = tasks[taskId];
        if (task.status != Status.Assigned) revert InvalidState();
        if (msg.sender != task.publisher && msg.sender != assignments[taskId].agentController) revert NotAuthorized();
        task.status = Status.Disputed;
        emit DisputeOpened(taskId, msg.sender);
    }

    function resolveDispute(bytes32 taskId, address payable recipient) external nonReentrant {
        if (msg.sender != disputeResolver) revert NotAuthorized();
        Task storage task = tasks[taskId];
        if (task.status != Status.Disputed) revert InvalidState();
        if (recipient != task.publisher && recipient != task.agent) revert InvalidAddress();
        uint256 amount = task.amount;
        task.amount = 0;
        task.status = recipient == task.publisher ? Status.Refunded : Status.Released;
        _endYieldEligibility(taskId);
        if (recipient == task.publisher) {
            (bool sent,) = recipient.call{value: amount}("");
            if (!sent) revert TransferFailed();
        } else {
            Assignment storage assignment = assignments[taskId];
            claimableEarnings[assignment.agentController][assignment.payout] += amount;
            totalClaimableEarnings += amount;
            emit EarningsAccrued(taskId, assignment.id, assignment.agentController, assignment.payout, amount);
        }
        emit DisputeResolved(taskId, recipient, amount);
    }

    function _endYieldEligibility(bytes32 taskId) private {
        uint256 amount = yieldEligiblePrincipal[taskId];
        if (amount == 0) return;
        yieldEligiblePrincipal[taskId] = 0;
        totalYieldEligiblePrincipal -= amount;
        emit YieldEligibilityChanged(taskId, amount, false);
    }

    function _recover(bytes32 digest, bytes calldata signature) private pure returns (address signer) {
        if (signature.length != 65) revert InvalidProof();
        bytes32 r;
        bytes32 s;
        uint8 v;
        assembly ("memory-safe") {
            r := calldataload(signature.offset)
            s := calldataload(add(signature.offset, 32))
            v := byte(0, calldataload(add(signature.offset, 64)))
        }
        if (uint256(s) > SECP256K1_HALF_ORDER || (v != 27 && v != 28)) revert InvalidProof();
        signer = ecrecover(digest, v, r, s);
        if (signer == address(0)) revert InvalidProof();
    }
}
