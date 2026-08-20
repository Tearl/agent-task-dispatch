// SPDX-License-Identifier: MIT
pragma solidity ^0.8.24;

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

    error AlreadyExists();
    error InvalidAddress();
    error InvalidAmount();
    error InvalidState();
    error NotAuthorized();
    error TransferFailed();

    event TaskCreated(bytes32 indexed taskId, address indexed publisher, uint256 amount);
    event AgentAssigned(bytes32 indexed taskId, address indexed agent);
    event FundsReleased(bytes32 indexed taskId, address indexed agent, uint256 amount);
    event FundsRefunded(bytes32 indexed taskId, address indexed publisher, uint256 amount);
    event DisputeOpened(bytes32 indexed taskId, address indexed openedBy);
    event DisputeResolved(bytes32 indexed taskId, address indexed recipient, uint256 amount);

    address public immutable disputeResolver;
    mapping(bytes32 taskId => Task task) public tasks;

    uint256 private locked = 1;

    constructor(address resolver) {
        if (resolver == address(0)) revert InvalidAddress();
        disputeResolver = resolver;
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

        tasks[taskId] = Task({
            publisher: msg.sender,
            agent: address(0),
            amount: msg.value,
            status: Status.Funded
        });

        emit TaskCreated(taskId, msg.sender, msg.value);
    }

    function assignAgent(bytes32 taskId, address agent) external {
        Task storage task = tasks[taskId];
        if (msg.sender != task.publisher) revert NotAuthorized();
        if (task.status != Status.Funded) revert InvalidState();
        if (agent == address(0)) revert InvalidAddress();

        task.agent = agent;
        task.status = Status.Assigned;
        emit AgentAssigned(taskId, agent);
    }

    function release(bytes32 taskId) external nonReentrant {
        Task storage task = tasks[taskId];
        if (msg.sender != task.publisher) revert NotAuthorized();
        if (task.status != Status.Assigned) revert InvalidState();

        uint256 amount = task.amount;
        address recipient = task.agent;
        task.status = Status.Released;
        task.amount = 0;

        (bool sent,) = payable(recipient).call{value: amount}("");
        if (!sent) revert TransferFailed();
        emit FundsReleased(taskId, recipient, amount);
    }

    function refund(bytes32 taskId) external nonReentrant {
        Task storage task = tasks[taskId];
        if (msg.sender != task.publisher) revert NotAuthorized();
        if (task.status != Status.Funded) revert InvalidState();

        uint256 amount = task.amount;
        task.status = Status.Refunded;
        task.amount = 0;

        (bool sent,) = payable(task.publisher).call{value: amount}("");
        if (!sent) revert TransferFailed();
        emit FundsRefunded(taskId, task.publisher, amount);
    }

    function openDispute(bytes32 taskId) external {
        Task storage task = tasks[taskId];
        if (task.status != Status.Assigned) revert InvalidState();
        if (msg.sender != task.publisher && msg.sender != task.agent) revert NotAuthorized();

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

        (bool sent,) = recipient.call{value: amount}("");
        if (!sent) revert TransferFailed();
        emit DisputeResolved(taskId, recipient, amount);
    }
}
