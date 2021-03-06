package aggregator

import (
	"crypto/ecdsa"
	"errors"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/rogercoll/optimisticrp"
	"github.com/sirupsen/logrus"
)

const MAX_TRANSACTIONS_BATCH = 502

type AggregatorNode struct {
	transactions     []optimisticrp.Transaction
	pendingDeposits  []optimisticrp.Deposit
	pendingWithdraws []optimisticrp.Withdraw
	accountsTrie     optimisticrp.Optimistic
	ethContract      optimisticrp.OptimisticSContract
	privKey          *ecdsa.PrivateKey
	onChainRoot      common.Hash
	log              *logrus.Entry
}

func New(newAccountsTrie optimisticrp.Optimistic, newEthContract optimisticrp.OptimisticSContract, privateKey *ecdsa.PrivateKey, logger *logrus.Logger) *AggregatorNode {
	aggregatorLogger := logger.WithFields(logrus.Fields{
		"service": "Aggregator",
	})
	return &AggregatorNode{
		accountsTrie: newAccountsTrie,
		ethContract:  newEthContract,
		privKey:      privateKey,
		log:          aggregatorLogger,
	}
}

//Sync with on-chain smart contract
func (ag *AggregatorNode) Synced() (bool, error) {
	onChainStateRoot, err := ag.onChainStateRoot()
	if err != nil {
		return false, err
	}
	if onChainStateRoot == ag.accountsTrie.StateRoot() {
		return true, nil
	}
	stateRoot, pendingDeposits, err := ag.computeAccountsTrie()
	if err != nil {
		return false, err
	}
	ag.pendingDeposits = pendingDeposits
	ag.log.WithFields(logrus.Fields{"StateRoot": stateRoot}).Info("Computed accounts state")
	ag.log.WithFields(logrus.Fields{"StateRoot": onChainStateRoot}).Info("OnChain accounts state")
	if stateRoot != onChainStateRoot {
		return false, fmt.Errorf("Aggregator was not able to compute a valid StateRoot")
	}
	return true, nil
}

//if sendBatch succeeds we should notify all user transactions
func (ag *AggregatorNode) sendBatch() error {
	prevStateRoot, err := ag.onChainStateRoot()
	if err != nil {
		return err
	}
	for _, deposit := range ag.pendingDeposits {
		err := ag.addFunds(deposit.From, deposit.Value)
		if err != nil {
			return err
		}
	}
	for _, withdraw := range ag.pendingWithdraws {
		err := ag.removeFunds(withdraw.From, withdraw.Value)
		if err != nil {
			return err
		}
	}
	for _, tx := range ag.transactions {
		_, err := ag.maliciousProcessTx(tx)
		if err != nil {
			return err
		}
	}
	b := optimisticrp.Batch{
		PrevStateRoot: prevStateRoot,
		StateRoot:     ag.accountsTrie.StateRoot(),
	}
	b.StateRoot = ag.accountsTrie.StateRoot()
	b.Transactions = ag.transactions
	txOpts, err := ag.ethContract.PrepareTxOptions(big.NewInt(0), big.NewInt(2), big.NewInt(2), ag.privKey)
	if err != nil {
		return err
	}
	_, err = ag.ethContract.NewBatch(b.SolidityFormat(), txOpts)
	if err != nil {
		return err
	}
	ag.transactions = nil
	return err
}

func (ag *AggregatorNode) ActualNonce(acc common.Address) (uint64, error) {
	val, err := ag.accountsTrie.GetAccount(acc)
	if err != nil {
		return 0, nil
	}
	return val.Nonce, nil
}

func (ag *AggregatorNode) ReceiveTransaction(tx optimisticrp.Transaction) error {
	ag.transactions = append(ag.transactions, tx)
	ag.log.WithFields(logrus.Fields{"From": tx.From, "To": tx.To, "Value:": tx.Value}).Debug("Appended transaction")
	if len(ag.transactions) == MAX_TRANSACTIONS_BATCH {
		ag.log.Info("Preparing and sending batch")
		if ok, err := ag.Synced(); ok {
			err := ag.sendBatch()
			if err != nil {
				return err
			}
		} else {
			ag.log.Fatal(err)
		}
	}
	return nil
}

//Should be private
func (ag *AggregatorNode) onChainStateRoot() (common.Hash, error) {
	return ag.ethContract.GetStateRoot()
}

//Malicious processTx which won't check if amount is negative
func (ag *AggregatorNode) maliciousProcessTx(transaction optimisticrp.Transaction) (common.Hash, error) {
	fromAcc, err := ag.accountsTrie.GetAccount(transaction.From)
	if err != nil {
		return common.Hash{}, err
	}
	toAcc, err := ag.accountsTrie.GetAccount(transaction.To)
	switch err.(type) {
	case nil:
	case *optimisticrp.AccountNotFound:
		toAcc = optimisticrp.Account{Balance: new(big.Int).SetUint64(0), Nonce: 0}
		ag.accountsTrie.UpdateAccount(transaction.To, toAcc)
	default:
		return common.Hash{}, err
	}
	if fromAcc.Balance.Cmp(transaction.Value) == -1 {
		ag.log.Warn("I am a malicious node, and that balance is negative but I won't check it")
		//setting balance to value as negative big.int cannot be rlp decoded
		fromAcc.Balance.Add(fromAcc.Balance, transaction.Value)
	}
	fromAcc.Balance.Sub(fromAcc.Balance, transaction.Value)
	toAcc.Balance.Add(toAcc.Balance, transaction.Value)
	fromAcc.Nonce++
	ag.accountsTrie.UpdateAccount(transaction.From, fromAcc)
	ag.log.WithFields(logrus.Fields{"Sender": transaction.From, "Remaining balance": fromAcc.Balance}).Debug("Processed transaction")
	return ag.accountsTrie.UpdateAccount(transaction.To, toAcc), nil
}

func (ag *AggregatorNode) addFunds(account common.Address, value *big.Int) error {
	acc, err := ag.accountsTrie.GetAccount(account)
	switch err.(type) {
	case nil:
	case *optimisticrp.AccountNotFound:
		newAcc := optimisticrp.Account{Balance: value, Nonce: 0}
		ag.accountsTrie.UpdateAccount(account, newAcc)
		return nil
	default:
		return err
	}
	acc.Balance.Add(acc.Balance, value)
	ag.accountsTrie.UpdateAccount(account, acc)
	return nil
}

func (ag *AggregatorNode) removeFunds(account common.Address, value *big.Int) error {
	acc, err := ag.accountsTrie.GetAccount(account)
	switch err.(type) {
	case nil:
	case *optimisticrp.AccountNotFound:
		newAcc := optimisticrp.Account{Balance: value, Nonce: 0}
		ag.accountsTrie.UpdateAccount(account, newAcc)
		return nil
	default:
		return err
	}
	acc.Balance.Sub(acc.Balance, value)
	ag.accountsTrie.UpdateAccount(account, acc)
	return nil
}

//Reads all transactions to the smart contracts and computes the whole accounts trie from scratch
func (ag *AggregatorNode) computeAccountsTrie() (common.Hash, []optimisticrp.Deposit, error) {
	optimisticTrie, ok := ag.accountsTrie.(*optimisticrp.OptimisticTrie)
	if ok != true {
		return common.Hash{}, nil, fmt.Errorf("This challenger implementation uses the OptimisticTrie object, if you are not, please develop your own challenger functions")
	}
	onChainData := make(chan interface{})
	go ag.ethContract.GetOnChainData(onChainData)
	stateRoot := common.Hash{}
	pendingDeposits := []optimisticrp.Deposit{}
	for methodData := range onChainData {
		switch input := methodData.(type) {
		case optimisticrp.SolidityBatch:
			batch, err := input.ToGolangFormat()
			if err != nil {
				return stateRoot, nil, err
			}
			ag.log.Info("New onChain Batch received")
			//if there is a new batch we MUST update the stateRoot with the previous deposits (rule 1.)
			isValid, err := ag.ethContract.IsStateRootValid(batch.StateRoot)
			if err != nil {
				return stateRoot, nil, err
			}
			onChainStateRoot, err := ag.ethContract.GetStateRoot()
			if err != nil {
				return stateRoot, nil, err
			}
			ag.log.Trace("Updating accounts state with new deposits")
			for _, deposit := range pendingDeposits {
				err := optimisticTrie.AddFunds(deposit.From, deposit.Value)
				if err != nil {
					return stateRoot, nil, err
				}
			}
			pendingDeposits = nil
			ag.log.Trace("Updating accounts state with last withdraws")
			for _, withdraw := range ag.pendingWithdraws {
				err := optimisticTrie.RemoveFunds(withdraw.From, withdraw.Value)
				if err != nil {
					return stateRoot, nil, err
				}
			}
			ag.pendingWithdraws = nil
			if isValid {
				ag.log.Info("Updating accounts state as the provided batch is valid, it shall not contain any error")
				for _, txInBatch := range batch.Transactions {
					stateRoot, err = optimisticTrie.ProcessTx(txInBatch)
					if err != nil {
						return stateRoot, nil, err
					}
				}
				//_ = v.sendFraudProof(common.HexToAddress("0x048C82fe2C85956Cf2872FBe32bE4AD06de3Db1E"))
			} else if !isValid && input.StateRoot == onChainStateRoot {
				tmpTrie, err := optimisticTrie.Copy()
				if err != nil {
					return stateRoot, nil, err
				}
				for _, txInBatch := range batch.Transactions {
					_, err := tmpTrie.ProcessTx(txInBatch)
					if err != nil {
						return stateRoot, nil, err
					}
				}
				//If after analyzing all transactions with the temporal Trie we don't get any error we can proceed updating the main Trie
				ag.log.Info("Last batch is valid but lock time has not expired, updating accounts state...")
				for _, txInBatch := range batch.Transactions {
					stateRoot, err = optimisticTrie.ProcessTx(txInBatch)
					if err != nil {
						return stateRoot, nil, err
					}
				}
			} else {
				ag.log.Debug("Skipping invalid onChain batch")
			}
		case optimisticrp.Deposit:
			ag.log.WithFields(logrus.Fields{"Account": input.From, "Value": input.Value}).Info("New onChain deposit")
			pendingDeposits = append(pendingDeposits, input)
		case optimisticrp.Withdraw:
			ag.log.WithFields(logrus.Fields{"Account": input.From, "Value": input.Value}).Info("New onChain withdraw")
			ag.pendingWithdraws = append(ag.pendingWithdraws, input)
		case error:
			return stateRoot, nil, input

		default:
			return common.Hash{}, nil, errors.New("There was an error while fetching onChain data")
		}
	}
	return stateRoot, pendingDeposits, nil
}
