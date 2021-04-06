// SPDX-License-Identifier: MIT

pragma solidity >=0.6.0 <=0.7.3;

import { Lib_MerkleTrie } from "./Lib_MerkleTrie.sol";
import { Lib_RLPReader } from "./Lib_RLPReader.sol";

contract Optimistic_Rollups {
    
    bytes32 public stateRoot;
    uint256 public immutable lock_time;
    uint256 public immutable required_bond;
    mapping(address => mapping(bytes32 => uint256)) private last_deposits;
    event New_Deposit(address user, bytes32 stateRoot, uint256 value);


    constructor(
        uint256 _lock_time,
        uint256 _required_bond
    ) public {
        stateRoot = bytes32(0);
        lock_time = _lock_time;
        required_bond = _required_bond;
    }
    
    function deposit() external payable {
        last_deposits[msg.sender][stateRoot] += msg.value;
        emit New_Deposit(msg.sender, stateRoot, msg.value);
    }
    
    
    //[248, 95, 160, 48, 90, 96, 180, 15, 169, 0, 12, 30, 160, 135, 133, 84, 61, 18, 113, 22, 62, 245, 86, 20, 148, 103, 136, 32, 124, 139, 204, 83, 60, 79, 110, 248, 60, 248, 58, 136, 13, 224, 182, 179, 167, 100, 0, 0, 133, 11, 164, 59, 116, 0, 148, 139, 80, 60, 161, 190, 245, 90, 144, 66, 118, 19, 143, 46, 166, 9, 6, 210, 197, 135, 129, 148, 4, 140, 130, 254, 44, 133, 149, 108, 242, 135, 47, 190, 50, 190, 74, 208, 109, 227, 219, 30, 1]
    function newBatch(bytes calldata _batch) external returns (string memory) {
        require(_batch.length > 0, "EMPTY_NEW_BATCH");
        Lib_RLPReader.RLPItem[] memory ls = Lib_RLPReader.readList(_batch);
        Lib_RLPReader.RLPItem memory _stateRoot = ls[0];
        require(stateRoot == abi.decode(Lib_RLPReader.readBytes(_stateRoot), (bytes32)), "INVALID_PREV_STATEROOT");
        Lib_RLPReader.RLPItem memory _newstateRoot = ls[1];
        stateRoot = abi.decode(Lib_RLPReader.readBytes(_newstateRoot), (bytes32));
        //RLPReader.RLPItem[] memory transactions = ls[2].toList();
        //Mirar si que hi hagi transactions
        //to = tx_data[2].toAddress();
    }
    
    //[160,48,90,96,180,15,169,0,12,30,160,135,133,84,61,18,113,22,62,245,86,20,148,103,136,32,124,139,204,83,60,79,110]
    function readHashRLP(bytes memory _hash) external returns (string memory) {
        stateRoot = abi.decode(Lib_RLPReader.readBytes(_hash), (bytes32));
    }
    
    function readHash(bytes calldata _hash) external returns (string memory) {
        stateRoot = abi.decode(_hash[:32], (bytes32));
    }
    
    function getStateRoot() public view returns (bytes32){
        return stateRoot;
    }
    

}

