// SPDX-License-Identifier: MIT
pragma solidity ^0.8.24;

/// @title Agent 任务原生资产托管合约
/// @notice 本地 MVP 使用的原生资产（例如 ETH）托管合约。
/// @dev 合约负责保管任务资金、验证平台的 Agent 选择凭证、结算正常任务或争议任务。
///      生产环境最终使用的资产、金额精度和签名人治理方式仍需在部署时决定。
///
/// 任务的主要状态流转：
/// None -> Funded -> Assigned -> Released
///                  |             （发布者验收，Agent 收益进入待提现余额）
///                  -> Disputed -> Released / Refunded
/// Funded -> Refunded
contract TaskEscrow {
    /// @notice 任务在托管合约中的生命周期状态。
    enum Status {
        None, // 任务不存在；mapping 中未写入时的默认值
        Funded, // 发布者已创建任务并把资金存入合约，但尚未选择 Agent
        Assigned, // 已验证选择凭证并锁定正式任务应付金额
        Released, // 已验收，或争议结果中 Agent 获得了资金
        Refunded, // 未分配任务已退款，或争议结果中 Agent 未获得资金
        Disputed // 任务已进入争议流程，等待争议解决者完成分配
    }

    /// @notice 每个任务最核心的托管信息。
    struct Task {
        address publisher; // 创建任务并支付资金的发布者
        address agent; // Agent 的收款地址（payout），不是 Agent 控制地址
        uint256 amount; // 当前仍锁在该任务中的本金；终态结算后归零
        Status status; // 当前任务状态
    }

    /// @notice 平台为“选择 Agent”操作签发的结构化凭证。
    /// @dev 所有字段都会被包含在 EIP-712 签名中，任一字段被篡改都会导致验签失败。
    ///      发布者通过亲自发送 selectAgent 交易，再次确认自己接受这些值。
    struct SelectionProof {
        bytes32 taskId; // 要绑定的链上任务 ID
        bytes32 assignmentId; // 本次唯一分配 ID，防止同一分配被多个任务重复使用
        address agentController; // 有权管理和提取收益的 Agent 控制地址
        address payout; // 收益最终只能发送到的绑定收款地址
        bytes32 overviewId; // 前置 overview/方案的标识
        bytes32 allocationId; // overview 抵扣分配 ID，只允许使用一次
        bytes32 quoteHash; // 报价内容哈希，避免在链上存储完整报价
        bytes32 taskSpecHash; // 任务规格哈希，用于证明签名对应哪一版需求
        uint64 matchRevision; // 匹配结果版本
        uint64 priceVersion; // 价格版本
        uint256 overviewPrice; // overview 原价格，用来约束抵扣额
        uint256 formalGrossPrice; // 正式任务抵扣前总价
        uint256 overviewCredit; // overview 可抵扣到正式任务的金额
        bytes32 policyHash; // 本次匹配/定价政策的哈希
        bytes32 nonce; // 平台签名随机数，全局只允许消费一次
        uint64 deadline; // 凭证过期时间（Unix 时间戳）
    }

    /// @notice 选择成功后永久保存的任务分配快照。
    /// @dev 与 SelectionProof 相比，不再保存只用于验签的 deadline、nonce 和 overviewPrice，
    ///      并额外保存计算后的 formalPayable（正式任务实际应付金额）。
    struct Assignment {
        bytes32 id; // assignmentId 的链上快照
        address agentController; // 可调用 withdrawEarnings 的控制地址
        address payout; // 提现时绑定的资金接收地址
        bytes32 overviewId; // overview 标识
        bytes32 allocationId; // 已消费的抵扣分配标识
        bytes32 quoteHash; // 报价哈希
        bytes32 taskSpecHash; // 任务规格哈希
        uint64 matchRevision; // 匹配结果版本
        uint64 priceVersion; // 价格版本
        uint256 formalGrossPrice; // 抵扣前正式任务价格
        uint256 overviewCredit; // 已使用的 overview 抵扣
        uint256 formalPayable; // 实际锁定金额 = formalGrossPrice - overviewCredit
        bytes32 policyHash; // 政策哈希
    }

    /// @notice 争议开启时冻结的一个潜在资金接收账户。
    /// @dev 这里固定冻结“谁能收钱”和“最多能收多少”，但尚不决定最终分配金额。
    struct FrozenLeaf {
        uint32 index; // 固定索引：0 为发布者，1 为 Agent
        address owner; // 该叶子绑定的账户
        uint256 cap; // 该账户可获得金额的上限
        uint8 accountKind; // 账户类型：0 = 发布者退款，1 = Agent 应收款
    }

    /// @notice 争议解决者提交的某个冻结账户的最终分配结果。
    struct DisputeAllocation {
        uint32 index; // 必须与冻结叶子的索引一致
        address owner; // 必须与冻结叶子的所有者一致
        uint256 amount; // 最终分配金额，不能超过对应 cap
    }

    /// @notice 一次争议冻结的元数据。
    struct DisputeFreeze {
        bytes32 root; // 对 taskId、全部叶子、费用接收方和费用上限的整体承诺哈希
        uint32 leafCount; // 冻结叶子总数；当前版本必须为 2
        address feeRecipient; // 争议处理费接收方；当前必须等于 disputeResolver
        uint256 feeCap; // 争议处理费上限
        uint64 finalizeAfter; // 最早可完成分配的时间，提供 1 天冷静期
        bool finalized; // 是否已经完成最终分配，防止重复结算
    }

    // 自定义错误比 revert 字符串更节省 Gas。调用方可通过 selector 判断失败原因。
    error AlreadyExists();
    error InvalidAddress();
    error InvalidAmount();
    error InvalidNonce();
    error InvalidProof();
    error InvalidState();
    error NotAuthorized();
    error ProofExpired();
    error TransferFailed();

    // 事件供 BFF、Engine 或索引服务重建链上任务和资金状态。
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
    event DisputeFrozen(bytes32 indexed taskId, bytes32 indexed root, uint32 leafCount, uint256 amount, uint256 feeCap, uint64 finalizeAfter);
    event DisputeLeafAllocated(bytes32 indexed taskId, uint32 indexed index, address indexed owner, uint256 amount);
    event DisputeAllocationFinalized(bytes32 indexed taskId, bytes32 indexed root, uint256 publisherAmount, uint256 agentAmount, uint256 feeAmount);

    // EIP-712 类型哈希和域参数。域中包含 chainId 与当前合约地址，防止跨链、跨合约重放签名。
    bytes32 public constant SELECTION_PROOF_TYPEHASH = keccak256("SelectionProof(bytes32 payloadHash)");
    bytes32 private constant EIP712_DOMAIN_TYPEHASH =
        keccak256("EIP712Domain(string name,string version,uint256 chainId,address verifyingContract)");
    bytes32 private constant NAME_HASH = keccak256("AgentTaskEscrow");
    bytes32 private constant VERSION_HASH = keccak256("1");
    // secp256k1 曲线阶的一半。要求 s 不大于该值，可拒绝可延展（malleable）签名。
    uint256 private constant SECP256K1_HALF_ORDER = 0x7fffffffffffffffffffffffffffffff5d576e7357a4501ddfe92f46681b20a0;

    address public immutable disputeResolver; // 唯一有权提交争议最终分配的地址
    address public immutable platformProofSigner; // 平台选择凭证的预期签名地址

    // 任务 ID => 托管任务；public mapping 会由 Solidity 自动生成只读 getter。
    mapping(bytes32 taskId => Task task) public tasks;
    mapping(bytes32 taskId => Assignment assignment) public assignments; // 每个任务最多一个分配
    mapping(bytes32 taskId => uint256 nonce) public workNonces; // 工作轮次版本，选择成功后从 1 开始

    // 三层防重放：平台随机数、分配 ID、overview 抵扣 ID 均只能使用一次。
    mapping(bytes32 nonce => bool used) public usedSelectionNonces;
    mapping(bytes32 assignmentId => bool used) public usedAssignmentIds;
    mapping(bytes32 allocationId => bool used) public usedOverviewCredits;

    // “拉取支付”账本：验收/争议结算只增加余额，不在同一交易中向 Agent 外部转账。
    mapping(address agentController => mapping(address payout => uint256 amount)) public claimableEarnings;
    // 已选择且仍未终态结算的本金，可供外围收益策略统计；本合约本身不执行投资。
    mapping(bytes32 taskId => uint256 amount) public yieldEligiblePrincipal;

    mapping(bytes32 taskId => DisputeFreeze freeze) public disputeFreezes; // 每个任务的争议冻结记录
    // 单独保存每片叶子的哈希，最终分配时逐片核对冻结时的身份、顺序和上限。
    mapping(bytes32 taskId => mapping(uint32 index => bytes32 leafHash)) public disputeLeafHashes;
    uint256 public totalClaimableEarnings; // 全部 Agent 尚未提现收益之和
    uint256 public totalYieldEligiblePrincipal; // 全部仍具收益资格的任务本金之和

    // 简单重入锁：1 = 未锁定，2 = 正在执行 nonReentrant 函数。
    uint256 private locked = 1;

    /// @param resolver 争议解决者地址，只能由它完成争议资金分配。
    /// @param proofSigner 平台 EIP-712 选择凭证的签名地址。
    constructor(address resolver, address proofSigner) {
        // immutable 地址一旦部署便不能修改，因此部署时必须拒绝零地址。
        if (resolver == address(0) || proofSigner == address(0)) revert InvalidAddress();
        disputeResolver = resolver;
        platformProofSigner = proofSigner;
    }

    /// @dev 防止外部转账回调再次进入受保护函数。
    modifier nonReentrant() {
        if (locked != 1) revert InvalidState();
        locked = 2;
        _;
        locked = 1;
    }

    /// @notice 创建任务，并将随交易发送的原生资产全部作为初始托管资金。
    /// @param taskId Engine/BFF 为任务生成的唯一 ID，不能为零且不能重复。
    /// @dev msg.value 就是托管金额；此时尚未选择 Agent，所以资金不具收益资格。
    function createTask(bytes32 taskId) external payable {
        if (taskId == bytes32(0)) revert InvalidState();
        if (msg.value == 0) revert InvalidAmount();
        if (tasks[taskId].status != Status.None) revert AlreadyExists();
        tasks[taskId] = Task(msg.sender, address(0), msg.value, Status.Funded);
        emit TaskCreated(taskId, msg.sender, msg.value);
    }

    /// @notice 原子地消费平台签名凭证、创建唯一分配、应用 overview 抵扣、锁定正式净价、
    ///         退回多余资金，并把工作轮次初始化为 1。
    /// @param proof 平台签名所覆盖的完整选择数据。
    /// @param signature platformProofSigner 对 EIP-712 摘要产生的 65 字节签名。
    function selectAgent(SelectionProof calldata proof, bytes calldata signature) external nonReentrant {
        Task storage task = tasks[proof.taskId];

        // 必须由任务发布者主动确认选择，并且任务只能从 Funded 进入 Assigned。
        if (msg.sender != task.publisher) revert NotAuthorized();
        if (task.status != Status.Funded) revert InvalidState();

        // 所有业务关键标识、地址与版本必须有效，避免产生无法追踪或无法结算的分配。
        if (
            proof.taskId == bytes32(0) || proof.assignmentId == bytes32(0) || proof.agentController == address(0)
                || proof.payout == address(0) || proof.overviewId == bytes32(0) || proof.allocationId == bytes32(0)
                || proof.quoteHash == bytes32(0) || proof.taskSpecHash == bytes32(0) || proof.matchRevision == 0
                || proof.priceVersion == 0 || proof.policyHash == bytes32(0)
        ) revert InvalidProof();

        // deadline 限制旧报价的有效期；三个 used mapping 防止签名或业务额度被重放。
        if (proof.deadline < block.timestamp) revert ProofExpired();
        if (proof.nonce == bytes32(0) || usedSelectionNonces[proof.nonce]) revert InvalidNonce();
        if (
            assignments[proof.taskId].id != bytes32(0) || usedAssignmentIds[proof.assignmentId]
                || usedOverviewCredits[proof.allocationId]
        ) revert AlreadyExists();

        // 抵扣不能超过 overview 的实际价格，也不能超过正式任务总价。
        if (
            proof.formalGrossPrice == 0 || proof.overviewCredit > proof.overviewPrice
                || proof.overviewCredit > proof.formalGrossPrice
        ) revert InvalidAmount();

        // 实际应付净价 = 正式总价 - overview 抵扣；原托管余额必须足以覆盖净价。
        uint256 formalPayable = proof.formalGrossPrice - proof.overviewCredit;
        if (task.amount < formalPayable) revert InvalidAmount();

        // 只有平台认可的完整 payload 才能进入链上分配。
        if (_recover(selectionProofDigest(proof), signature) != platformProofSigner) revert InvalidProof();

        uint256 excess = task.amount - formalPayable;

        // Checks-Effects-Interactions：先消费所有一次性标识并更新状态，再进行下方退款外部调用。
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

        // 发布者预存资金若高于实际应付净价，立即退回差额。
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

    /// @notice 以 compare-and-swap 方式推进工作轮次，防止并发或重放授权跳过 nonce。
    /// @param expectedCurrentNonce 调用者认为的当前 nonce；只有与链上值完全一致才能推进。
    function advanceWorkNonce(bytes32 taskId, uint256 expectedCurrentNonce) external {
        Task storage task = tasks[taskId];
        if (msg.sender != task.publisher) revert NotAuthorized();
        if (task.status != Status.Assigned) revert InvalidState();
        if (workNonces[taskId] != expectedCurrentNonce || expectedCurrentNonce == 0) revert InvalidNonce();
        uint256 nextNonce = expectedCurrentNonce + 1;
        workNonces[taskId] = nextNonce;
        emit WorkNonceAdvanced(taskId, assignments[taskId].id, nextNonce);
    }

    /// @notice 将 SelectionProof 的全部 ABI 编码字段压缩成一个 payload 哈希。
    /// @dev 使用 abi.encode 而不是 abi.encodePacked，避免不同动态布局产生歧义。
    function selectionPayloadHash(SelectionProof calldata proof) public pure returns (bytes32) {
        return keccak256(abi.encode(proof));
    }

    /// @notice 计算平台实际需要签名或链上需要恢复签名人的 EIP-712 摘要。
    function selectionProofDigest(SelectionProof calldata proof) public view returns (bytes32) {
        bytes32 structHash = keccak256(abi.encode(SELECTION_PROOF_TYPEHASH, selectionPayloadHash(proof)));
        // \x19\x01 是 EIP-712 固定前缀。
        return keccak256(abi.encodePacked("\x19\x01", domainSeparator(), structHash));
    }

    /// @notice 计算当前链与当前合约专属的 EIP-712 域分隔符。
    function domainSeparator() public view returns (bytes32) {
        return keccak256(abi.encode(EIP712_DOMAIN_TYPEHASH, NAME_HASH, VERSION_HASH, block.chainid, address(this)));
    }

    /// @notice 便捷查询某个任务绑定的 assignment ID。
    function assignmentIdOf(bytes32 taskId) external view returns (bytes32) {
        return assignments[taskId].id;
    }

    /// @notice 发布者验收整个 V1 正式交付包；未使用的工作轮次不会按比例退款。
    /// @dev 验收后资金先进入独立的待提现账本，而不是立即调用 Agent 地址，降低重入和失败风险。
    function accept(bytes32 taskId) external nonReentrant {
        _accept(taskId);
    }

    /// @notice 为旧客户端保留的验收入口，与 accept 的行为完全相同。
    function release(bytes32 taskId) external nonReentrant {
        _accept(taskId);
    }

    /// @dev accept/release 共用的实际验收逻辑。
    function _accept(bytes32 taskId) private {
        Task storage task = tasks[taskId];
        if (msg.sender != task.publisher) revert NotAuthorized();
        if (task.status != Status.Assigned) revert InvalidState();
        uint256 amount = task.amount;
        Assignment storage assignment = assignments[taskId];

        // 先进入终态并把任务本金归零，避免同一任务再次验收或参与其他结算。
        task.status = Status.Released;
        task.amount = 0;
        _endYieldEligibility(taskId);

        // Pull Payment：将收益记到“控制地址 + 固定收款地址”名下，稍后由控制地址主动提现。
        claimableEarnings[assignment.agentController][assignment.payout] += amount;
        totalClaimableEarnings += amount;
        emit FundsReleased(taskId, assignment.payout, amount);
        emit EarningsAccrued(taskId, assignment.id, assignment.agentController, assignment.payout, amount);
    }

    /// @notice Agent 控制地址将自己的待提现收益发送到绑定的 payout 地址。
    /// @param payout 选择凭证绑定的收款地址，也是实际收到原生资产的地址。
    /// @param amount 本次提现金额，允许部分提现。
    /// @dev msg.sender 自动作为 agentController 查询余额，因此其他地址不能代领。
    function withdrawEarnings(address payable payout, uint256 amount) external nonReentrant {
        if (payout == address(0)) revert InvalidAddress();
        if (amount == 0) revert InvalidAmount();
        uint256 available = claimableEarnings[msg.sender][payout];
        if (amount > available) revert InvalidAmount();

        // 先扣减内部账本再转账，配合 nonReentrant 防止回调重复提现。
        claimableEarnings[msg.sender][payout] = available - amount;
        totalClaimableEarnings -= amount;
        (bool sent,) = payout.call{value: amount}("");
        if (!sent) revert TransferFailed();
        emit EarningsWithdrawn(msg.sender, payout, amount);
    }

    /// @notice 发布者撤回尚未选择 Agent 的任务资金。
    /// @dev 只允许 Funded 状态；Assigned 后必须走验收或争议流程，不能单方面退款。
    function refund(bytes32 taskId) external nonReentrant {
        Task storage task = tasks[taskId];
        if (msg.sender != task.publisher) revert NotAuthorized();
        if (task.status != Status.Funded) revert InvalidState();
        uint256 amount = task.amount;

        // 先写终态和清零，再执行对发布者地址的外部转账。
        task.status = Status.Refunded;
        task.amount = 0;
        _endYieldEligibility(taskId);
        (bool sent,) = payable(task.publisher).call{value: amount}("");
        if (!sent) revert TransferFailed();
        emit FundsRefunded(taskId, task.publisher, amount);
    }

    /// @notice 已弃用的旧争议入口，仅保留 ABI 兼容性并始终回退。
    /// @dev 让旧客户端“安全失败”，避免绕过新版冻结信息后创建无法完整结算的争议。
    function openDispute(bytes32) external pure {
        revert InvalidState();
    }

    /// @notice 开启争议并冻结完整、稳定的资金分配范围。
    /// @dev 在最终裁决金额产生之前，每个叶子已经绑定索引、所有者、上限和账户类型，
    ///      防止争议解决者结算时偷偷替换收款人或扩大可分配范围。
    function freezeDispute(bytes32 taskId, FrozenLeaf[] calldata leaves, address feeRecipient, uint256 feeCap)
        external
    {
        Task storage task = tasks[taskId];

        // 发布者或 Agent 控制地址可以发起，但 Agent 的 payout 地址本身没有管理权限。
        if (task.status != Status.Assigned) revert InvalidState();
        if (msg.sender != task.publisher && msg.sender != assignments[taskId].agentController) revert NotAuthorized();

        // 当前版本固定两个账户；争议费只能给 disputeResolver，且上限不能超过本金。
        if (leaves.length != 2 || feeRecipient != disputeResolver || feeCap > task.amount) revert InvalidProof();

        // 叶子 0 必须是发布者退款账户，叶子 1 必须是 Agent payout；两者 cap 均为完整本金。
        // cap 是各自的独立上限，不表示两者可同时取得完整本金，最终还有总额守恒检查。
        if (
            leaves[0].index != 0 || leaves[0].owner != task.publisher || leaves[0].accountKind != 0
                || leaves[0].cap != task.amount || leaves[1].index != 1
                || leaves[1].owner != assignments[taskId].payout || leaves[1].accountKind != 1
                || leaves[1].cap != task.amount
        ) revert InvalidProof();

        // root 是整组冻结数据的承诺；每个任务只允许冻结一次。
        bytes32 root = keccak256(abi.encode(taskId, leaves, feeRecipient, feeCap));
        if (root == bytes32(0) || disputeFreezes[taskId].root != bytes32(0)) revert AlreadyExists();

        // 冻结后等待 1 天才能结算，为外围系统核对或响应异常留出时间。
        uint64 finalizeAfter = uint64(block.timestamp + 1 days);
        disputeFreezes[taskId] = DisputeFreeze(root, uint32(leaves.length), feeRecipient, feeCap, finalizeAfter, false);

        // 保存单叶哈希，以便结算时验证每个 allocation 对应原先冻结的账户。
        for (uint32 i; i < leaves.length; i++) {
            disputeLeafHashes[taskId][i] = keccak256(abi.encode(leaves[i]));
        }
        task.status = Status.Disputed;
        emit DisputeOpened(taskId, msg.sender);
        emit DisputeFrozen(taskId, root, uint32(leaves.length), task.amount, feeCap, finalizeAfter);
    }

    /// @notice 由争议解决者一次性提交全部冻结叶子的最终资金分配。
    /// @dev 稳定索引、所有者、上限、费用接收方和总金额都会在链上重新检查；
    ///      allocations 必须按索引顺序完整覆盖所有叶子，不能漏项或重复。
    function finalizeDisputeAllocation(bytes32 taskId, DisputeAllocation[] calldata allocations, uint256 feeAmount)
        external
        nonReentrant
    {
        if (msg.sender != disputeResolver) revert NotAuthorized();
        Task storage task = tasks[taskId];
        DisputeFreeze storage frozen = disputeFreezes[taskId];

        // 必须已经冻结、未结算，并且 1 天等待期已经结束。
        if (task.status != Status.Disputed || frozen.root == bytes32(0) || frozen.finalized || block.timestamp < frozen.finalizeAfter) revert InvalidState();
        if (allocations.length != frozen.leafCount || feeAmount > frozen.feeCap) revert InvalidProof();
        uint256 publisherAmount;
        uint256 agentAmount;

        // 当前循环按 index 精确对应冻结叶子：0 -> 发布者，1 -> Agent。
        for (uint32 i; i < allocations.length; i++) {
            DisputeAllocation calldata allocation = allocations[i];
            if (allocation.index != i) revert InvalidProof();
            uint8 accountKind = i == 0 ? 0 : 1;
            uint256 cap = task.amount;
            if (disputeLeafHashes[taskId][i] != keccak256(abi.encode(FrozenLeaf(i, allocation.owner, cap, accountKind)))) {
                revert InvalidProof();
            }
            if (allocation.amount > cap) revert InvalidAmount();
            if (i == 0) publisherAmount = allocation.amount;
            else agentAmount = allocation.amount;
        }

        // 资金守恒：发布者退款 + Agent 收益 + 争议费必须恰好等于任务剩余本金。
        if (publisherAmount + agentAmount + feeAmount != task.amount) revert InvalidAmount();

        // 在任何转账前锁定最终结果并清零本金，防止重入和重复结算。
        frozen.finalized = true;
        task.amount = 0;

        // 只要 Agent 获得非零金额，任务记为 Released；否则记为 Refunded。
        task.status = agentAmount == 0 ? Status.Refunded : Status.Released;
        _endYieldEligibility(taskId);

        // Agent 部分继续使用 pull payment，不由争议解决者直接推送给 payout。
        if (agentAmount != 0) {
            Assignment storage assignment = assignments[taskId];
            claimableEarnings[assignment.agentController][assignment.payout] += agentAmount;
            totalClaimableEarnings += agentAmount;
            emit EarningsAccrued(taskId, assignment.id, assignment.agentController, assignment.payout, agentAmount);
        }

        // 发布者退款和争议费直接发送；任一转账失败会回退整笔交易及上面的状态修改。
        if (publisherAmount != 0) {
            (bool publisherSent,) = payable(task.publisher).call{value: publisherAmount}("");
            if (!publisherSent) revert TransferFailed();
        }
        if (feeAmount != 0) {
            (bool feeSent,) = payable(frozen.feeRecipient).call{value: feeAmount}("");
            if (!feeSent) revert TransferFailed();
        }
        emit DisputeLeafAllocated(taskId, 0, task.publisher, publisherAmount);
        emit DisputeLeafAllocated(taskId, 1, assignments[taskId].payout, agentAmount);
        emit DisputeAllocationFinalized(taskId, frozen.root, publisherAmount, agentAmount, feeAmount);
    }

    /// @notice 已弃用的“全部资金给单一接收人”争议接口，现已主动禁用。
    /// @dev 所有争议都必须经过冻结叶子的完整性与资金守恒检查。
    function resolveDispute(bytes32, address payable) external pure {
        revert InvalidState();
    }

    /// @dev 任务进入终态时，移除它的收益资格本金并同步维护全局汇总值。
    function _endYieldEligibility(bytes32 taskId) private {
        uint256 amount = yieldEligiblePrincipal[taskId];
        if (amount == 0) return;
        yieldEligiblePrincipal[taskId] = 0;
        totalYieldEligiblePrincipal -= amount;
        emit YieldEligibilityChanged(taskId, amount, false);
    }

    /// @dev 从 65 字节 ECDSA 签名中恢复签名地址，并拒绝非规范签名。
    ///      签名布局为 r(32 bytes) || s(32 bytes) || v(1 byte)。
    function _recover(bytes32 digest, bytes calldata signature) private pure returns (address signer) {
        if (signature.length != 65) revert InvalidProof();
        bytes32 r;
        bytes32 s;
        uint8 v;
        assembly ("memory-safe") {
            // signature 是 calldata 切片，offset 指向其首个数据字节。
            r := calldataload(signature.offset)
            s := calldataload(add(signature.offset, 32))
            v := byte(0, calldataload(add(signature.offset, 64)))
        }

        // EIP-2 low-s 规则消除签名可延展性；Ethereum 标准 v 只能为 27 或 28。
        if (uint256(s) > SECP256K1_HALF_ORDER || (v != 27 && v != 28)) revert InvalidProof();
        signer = ecrecover(digest, v, r, s);
        // ecrecover 验证失败时返回零地址而不是 revert，因此这里必须显式检查。
        if (signer == address(0)) revert InvalidProof();
    }
}
