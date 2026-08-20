// SPDX-License-Identifier: MIT
pragma solidity ^0.8.24;

import {TaskEscrow} from "../src/TaskEscrow.sol";

interface Vm {
    function deal(address account, uint256 newBalance) external;
}

contract TaskEscrowTest {
    Vm private constant vm = Vm(address(uint160(uint256(keccak256("hevm cheat code")))));

    TaskEscrow private escrow;
    address private constant agent = address(0xBEEF);

    receive() external payable {}

    function setUp() public {
        escrow = new TaskEscrow(address(this));
        vm.deal(address(this), 10 ether);
    }

    function testCreateAssignAndRelease() public {
        bytes32 taskId = keccak256("task-release");
        escrow.createTask{value: 1 ether}(taskId);
        escrow.assignAgent(taskId, agent);

        uint256 beforeBalance = agent.balance;
        escrow.release(taskId);

        require(agent.balance == beforeBalance + 1 ether, "agent was not paid");
        (,, uint256 amount, TaskEscrow.Status status) = escrow.tasks(taskId);
        require(amount == 0, "escrow amount was not cleared");
        require(status == TaskEscrow.Status.Released, "task was not released");
    }

    function testRefundBeforeAssignment() public {
        bytes32 taskId = keccak256("task-refund");
        escrow.createTask{value: 1 ether}(taskId);

        uint256 balanceBeforeRefund = address(this).balance;
        escrow.refund(taskId);

        require(address(this).balance == balanceBeforeRefund + 1 ether, "publisher was not refunded");
        (,, uint256 amount, TaskEscrow.Status status) = escrow.tasks(taskId);
        require(amount == 0, "escrow amount was not cleared");
        require(status == TaskEscrow.Status.Refunded, "task was not refunded");
    }
}
