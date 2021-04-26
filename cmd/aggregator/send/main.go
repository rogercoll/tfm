package main

import (
	"math/big"
	"os"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/ethdb/memorydb"
	"github.com/ethereum/go-ethereum/trie"
	"github.com/rogercoll/optimisticrp"
	"github.com/rogercoll/optimisticrp/aggregator"
	"github.com/rogercoll/optimisticrp/bridge"
	"github.com/rogercoll/optimisticrp/cmd"
	"github.com/sirupsen/logrus"
)

var addrAccount1 = common.HexToAddress("0x3168444b98B4Bd55976137DdeEeC7A1d7BF322d3")
var addrAccount2 = common.HexToAddress("0x9185eAE1c5AD845137AaDf34a955e1D676fE421B")

func main() {
	var logger = logrus.New()
	logger.SetOutput(os.Stdout)
	logger.SetLevel(logrus.DebugLevel)
	client, err := ethclient.Dial("http://127.0.0.1:8545")
	if err != nil {
		logger.Fatal(err)
	}
	logger.Info("Connected to the ETH client")
	mybridge, err := bridge.New(common.HexToAddress(cmd.ContractAddr), client, logger)
	if err != nil {
		logger.Fatal(err)
	}
	var (
		diskdb = memorydb.New()
		triedb = trie.NewDatabase(diskdb)
	)
	tr, err := optimisticrp.NewTrie(triedb)
	if err != nil {
		logger.Fatal(err)
	}
	privateKey, err := crypto.HexToECDSA(cmd.AggregatorPriv)
	if err != nil {
		logger.Fatal(err)
	}
	myaggregator := aggregator.New(tr, mybridge, privateKey, logger)
	syn, err := myaggregator.Synced()
	if err != nil {
		logger.Fatal(err)
	} else if syn == false {
		logger.Fatal("Was not able to syncronize")
	}
	logger.Info("Successfully syncronized with on-chain data")
	for i := 0; i < aggregator.MAX_TRANSACTIONS_BATCH; i++ {
		tx := optimisticrp.Transaction{Value: big.NewInt(1e+16), Gas: big.NewInt(1e+18), To: addrAccount2, From: addrAccount1}
		err := myaggregator.ReceiveTransaction(tx)
		if err != nil {
			logger.Fatal(err)
		}
	}
}
