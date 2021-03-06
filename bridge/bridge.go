package bridge

import (
	"context"
	"crypto/ecdsa"
	"fmt"
	"log"
	"math/big"
	"strings"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/rogercoll/optimisticrp"
	store "github.com/rogercoll/optimisticrp/contracts"
	"github.com/sirupsen/logrus"
)

type Bridge struct {
	oriContract *store.Contracts
	oriAddr     common.Address
	client      *ethclient.Client
	log         *logrus.Entry
}

func New(oriAddr common.Address, ethClient *ethclient.Client, logger *logrus.Logger) (*Bridge, error) {
	bridgeLogger := logger.WithFields(logrus.Fields{
		"service": "Bridge",
	})
	instance, err := store.NewContracts(oriAddr, ethClient)
	if err != nil {
		return nil, err
	}
	return &Bridge{instance, oriAddr, ethClient, bridgeLogger}, nil
}

func (b *Bridge) Client() *ethclient.Client {
	return b.client
}

func (b *Bridge) GetStateRoot() (common.Hash, error) {
	onChainStateRoot, err := b.oriContract.StateRoot(nil)
	if err != nil {
		return common.Hash{}, err
	}
	return onChainStateRoot, nil
}

func (b *Bridge) NewBatch(batch optimisticrp.SolidityBatch, txOpts *bind.TransactOpts) (*types.Transaction, error) {
	result, err := rlp.EncodeToBytes(batch)
	if err != nil {
		return nil, err
	}
	b.log.WithFields(logrus.Fields{"Bytes": len(result)}).Warn("Batch size")
	txresult, err := b.oriContract.NewBatch(txOpts, result)
	if err != nil {
		return nil, err
	}
	b.log.Info("New batch was successfully submited onChain")
	return txresult, nil
}

func (b *Bridge) OriAddr() common.Address {
	return b.oriAddr
}

func (b *Bridge) FraudProof(txOpts *bind.TransactOpts, address, value, proof, stateRoot []byte, lastBatch optimisticrp.SolidityBatch) (*types.Transaction, error) {
	var array [32]byte
	copy(array[:], stateRoot[:32])
	result, err := rlp.EncodeToBytes(lastBatch)
	if err != nil {
		return nil, err
	}
	txresult, err := b.oriContract.ProveFraud(txOpts, address, value, proof, array, result)
	if err != nil {
		return nil, err
	}
	b.log.Info("Fraud proof was successfully submited onChain")
	return txresult, nil
}

func (b *Bridge) Withdraw(txOpts *bind.TransactOpts, address, value, proof, stateRoot []byte) (*types.Transaction, error) {
	var array [32]byte
	copy(array[:], stateRoot[:32])
	txresult, err := b.oriContract.Withdraw(txOpts, address, value, proof, array)
	if err != nil {
		return nil, err
	}
	b.log.Info("Withdraw was successfully submited onChain")
	return txresult, nil
}

func (b *Bridge) Bond(txOpts *bind.TransactOpts) (*types.Transaction, error) {
	txresult, err := b.oriContract.Bond(txOpts)
	if err != nil {
		return nil, err
	}
	b.log.Info("Bond was successful")
	return txresult, nil
}

func (b *Bridge) Deposit(txOpts *bind.TransactOpts) (*types.Transaction, error) {
	txresult, err := b.oriContract.Deposit(txOpts)
	if err != nil {
		return nil, err
	}
	b.log.Info("Deposit to onChain smart contract done successfully")
	return txresult, nil
}

func (b *Bridge) RemainingFraudPeriod() (*big.Int, error) {
	remaining, err := b.oriContract.RemainingProofTime(nil)
	if err != nil {
		return nil, err
	}
	return remaining, nil
}

func (b *Bridge) IsStateRootValid(state common.Hash) (bool, error) {
	validStateRoot, err := b.oriContract.ValidStateRoots(nil, state)
	if err != nil {
		return false, err
	}
	return validStateRoot, nil
}

//Reads all transactions to the smart contracts and computes the whole accounts trie from scratch
//This implementation is used for local chains, few blocks. In production (main chain) you shall use an ingestion service to get all the transactions of a given address.
func (b *Bridge) GetOnChainData(dataChannel chan<- interface{}) {
	defer close(dataChannel)
	header, err := b.client.HeaderByNumber(context.Background(), nil)
	if err != nil {
		dataChannel <- err
	}
	myAbi, err := abi.JSON(strings.NewReader(store.ContractsABI))
	if err != nil {
		dataChannel <- err
	}
	b.log.Debug(fmt.Sprintf("Analyzing %v blocks\n", header.Number))
	for i := int64(0); i <= header.Number.Int64(); i++ {
		block, err := b.client.BlockByNumber(context.Background(), big.NewInt(i))
		if err != nil {
			dataChannel <- err
		}
		for _, tx := range block.Transactions() {
			//if tx.To() == nil => Contract creation
			if tx.To() != nil && (*(tx.To()) == b.oriAddr) {
				txReceipt, err := b.client.TransactionReceipt(context.Background(), tx.Hash())
				if err != nil {
					dataChannel <- err
				}
				if txReceipt.Status == 1 {
					inputData := tx.Data()
					sigdata, argdata := inputData[:4], inputData[4:]
					method, err := myAbi.MethodById(sigdata)
					if err != nil {
						dataChannel <- err
					}
					if method.Name == "newBatch" {
						data, err := method.Inputs.UnpackValues(argdata)
						if err != nil {
							dataChannel <- err
						}
						var batch optimisticrp.SolidityBatch
						err = rlp.DecodeBytes(data[0].([]byte), &batch)
						if err != nil {
							b.log.Warn("Unable to unmarshal batch from transaction")
							continue
						}
						dataChannel <- batch
					} else if method.Name == "deposit" {
						msg, err := tx.AsMessage(types.NewEIP155Signer(tx.ChainId()))
						if err != nil {
							dataChannel <- err
						}
						dataChannel <- optimisticrp.Deposit{msg.From(), tx.Value()}
					} else if method.Name == "withdraw" {
						data, err := method.Inputs.UnpackValues(argdata)
						if err != nil {
							dataChannel <- err
						}
						var acc optimisticrp.SolidityAccount
						err = rlp.DecodeBytes(data[1].([]byte), &acc)
						if err != nil {
							dataChannel <- err
						}
						msg, err := tx.AsMessage(types.NewEIP155Signer(tx.ChainId()))
						if err != nil {
							dataChannel <- err
						}
						goFormat, err := acc.ToGolangFormat()
						if err != nil {
							dataChannel <- err
						}
						dataChannel <- optimisticrp.Withdraw{msg.From(), goFormat.Balance}
					}
				}
			}
		}
	}
	b.log.Info("All blocks analized")
}

func (b *Bridge) GetPendingDeposits(depChannel chan<- interface{}) {
	defer close(depChannel)
	header, err := b.client.HeaderByNumber(context.Background(), nil)
	if err != nil {
		depChannel <- err
	}
	myAbi, err := abi.JSON(strings.NewReader(store.ContractsABI))
	if err != nil {
		depChannel <- err
	}
	for i := header.Number.Int64(); i >= 0; i-- {
		block, err := b.client.BlockByNumber(context.Background(), big.NewInt(i))
		if err != nil {
			depChannel <- err
		}
		for _, tx := range block.Transactions() {
			//if tx.To() == nil => Contract creation
			if tx.To() != nil && (*(tx.To()) == b.oriAddr) {
				txReceipt, err := b.client.TransactionReceipt(context.Background(), tx.Hash())
				if err != nil {
					depChannel <- err
				}
				//only proceed if the transaction was not reverted => valid == 1
				if txReceipt.Status == 1 {
					inputData := tx.Data()
					sigdata, _ := inputData[:4], inputData[4:]
					method, err := myAbi.MethodById(sigdata)
					if err != nil {
						depChannel <- err
					}
					if method.Name == "deposit" {
						msg, err := tx.AsMessage(types.NewEIP155Signer(tx.ChainId()))
						if err != nil {
							log.Fatal(err)
						}
						depChannel <- optimisticrp.Deposit{msg.From(), tx.Value()}
					} else if method.Name == "newBatch" {
						depChannel <- err
					}
				}
			}
		}
	}
}

//If gasPrice == -1 => ask to the client suggested gas price
func (b *Bridge) PrepareTxOptions(value, gasLimit, gasPrice *big.Int, privKey *ecdsa.PrivateKey) (*bind.TransactOpts, error) {
	var err error
	if gasPrice.Cmp(big.NewInt(-1)) == 0 {
		gasPrice, err = b.client.SuggestGasPrice(context.Background())
		if err != nil {
			return nil, err
		}
	}
	publicKey := privKey.Public()
	publicKeyECDSA, ok := publicKey.(*ecdsa.PublicKey)
	if !ok {
		log.Fatal("error casting public key to ECDSA")
	}

	fromAddress := crypto.PubkeyToAddress(*publicKeyECDSA)

	nonce, err := b.client.PendingNonceAt(context.Background(), fromAddress)
	if err != nil {
		log.Fatal(err)
	}
	auth := bind.NewKeyedTransactor(privKey)
	auth.Nonce = new(big.Int).SetUint64(nonce)
	auth.Value = value              // in wei
	auth.GasLimit = uint64(5000000) // in units
	auth.GasPrice = gasPrice
	return auth, nil
}
