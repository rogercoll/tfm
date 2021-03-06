package main

import (
	"os"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/ethdb/memorydb"
	"github.com/ethereum/go-ethereum/trie"
	"github.com/rogercoll/optimisticrp"
	"github.com/rogercoll/optimisticrp/bridge"
	"github.com/rogercoll/optimisticrp/challenger"
	"github.com/rogercoll/optimisticrp/cmd"
	"github.com/sirupsen/logrus"
)

var addrAccount1 = common.HexToAddress("0x048C82fe2C85956Cf2872FBe32bE4AD06de3Db1E")
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
	privateKey, err := crypto.HexToECDSA(cmd.ChallengerPriv)
	if err != nil {
		logger.Fatal(err)
	}
	challengerNode := challenger.New(tr, mybridge, privateKey, logger)
	logs := make(chan interface{})
	go challengerNode.VerifyOnChainData(logs)
	for {
		select {
		case input := <-logs:
			switch vlog := input.(type) {
			case error:
				logger.Fatal(vlog)
			default:
				logger.Println(vlog)
			}
		}
	}
}
