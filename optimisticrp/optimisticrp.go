package optimisticrp

import (
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethdb/memorydb"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/ethereum/go-ethereum/trie"
	"log"
	"math/big"
)

type Oprollups struct {
	AccountsTrie *trie.Trie
	StateRoot    common.Hash
	//ORI Addr => Optimistic Rollups Implementation Smart Contract Address
	OriAddr  string
	NewBatch Batch
}

func New(oriAddr string) (*Oprollups, error) {
	var (
		diskdb = memorydb.New()
		triedb = trie.NewDatabase(diskdb)
	)
	tr, err := trie.New(common.Hash{}, triedb)
	if err != nil {
		return nil, err
	}
	return &Oprollups{tr, tr.Hash(), oriAddr, Batch{}}, nil
}

func (opr *Oprollups) GetAccount(address common.Address) (Account, error) {
	fBytes := opr.AccountsTrie.Get(address.Bytes())
	var acc Account
	if err := rlp.DecodeBytes(fBytes, &acc); err != nil {
		return Account{}, err
	}
	return acc, nil
}

func (opr *Oprollups) UpdateAccount(address common.Address, acc Account) error {
	val, err := rlp.EncodeToBytes(acc)
	if err != nil {
		return err
	}
	opr.AccountsTrie.Update(address.Bytes(), val)
	return nil
}

//https://github.com/ethereum/go-ethereum/blob/bbfb1e4008a359a8b57ec654330c0e674623e52f/core/types/transaction.go#L68
func (opr *Oprollups) NewOptimisticTx(to, from common.Address, value, gas *big.Int) error {
	fromAcc, err := opr.GetAccount(from)
	if err != nil {
		return err
	}
	toAcc, err := opr.GetAccount(to)
	if err != nil {
		return err
	}
	fromAcc.Nonce += 1
	tx := Transaction{
		From:  from,
		To:    to,
		Value: value,
		Gas:   gas,
		Nonce: fromAcc.Nonce,
	}
	fromAcc.Balance.Sub(fromAcc.Balance, value)
	toAcc.Balance.Add(toAcc.Balance, value)
	err = opr.UpdateAccount(from, fromAcc)
	if err != nil {
		return err
	}
	err = opr.UpdateAccount(to, toAcc)
	opr.StateRoot = opr.AccountsTrie.Hash()
	opr.NewBatch.StateRoot = opr.AccountsTrie.Hash()
	opr.NewBatch.Transactions = append(opr.NewBatch.Transactions, tx)
	return nil
}

func (opr *Oprollups) AddAccount(addr common.Address) error {
	acc := Account{Balance: new(big.Int).SetUint64(10e+18), Nonce: 0}
	err := opr.UpdateAccount(addr, acc)
	return err
}

func (opr *Oprollups) SendBatch() error {
	result, _ := opr.NewBatch.MarshalBinary()
	log.Println(result)
	log.Println(len(result))
	return nil
}
