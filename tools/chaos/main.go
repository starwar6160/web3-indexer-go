package main

import (
	"context"
	"crypto/ecdsa"
	"fmt"
	"log"
	"math/big"
	"math/rand"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
)

// Anvil 默认私钥列表
var privateKeys = []string{
	"ac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80",
	"59c6995e998f97a5a0044966f0945389dc9e86dae88c7a8412f4603b6b78690d",
	"5de4111afa1a4b94908f83103eb1f1706367c2e68ca870fc3fb9a804cdab365a",
}

type Account struct {
	PrivateKey *ecdsa.PublicKey
	Address    common.Address
	Key        *ecdsa.PrivateKey
	Nonce      uint64
	Mu         sync.Mutex
}

func main() {
	client, err := ethclient.Dial("http://127.0.0.1:8545")
	if err != nil {
		log.Fatal("❌ Cannot connect to Anvil:", err)
	}

	chainID, _ := client.ChainID(context.Background())
	accounts := make([]*Account, len(privateKeys))

	for i, pkHex := range privateKeys {
		pk, _ := crypto.HexToECDSA(pkHex)
		addr := crypto.PubkeyToAddress(pk.PublicKey)
		nonce, _ := client.PendingNonceAt(context.Background(), addr)
		accounts[i] = &Account{
			Address: addr,
			Key:     pk,
			Nonce:   nonce,
		}
	}

	// 🚀 高频注入：每 100ms 触发一次
	ticker := time.NewTicker(100 * time.Millisecond)
	fmt.Println("🔥 Chaos Injector Active. Flooding Anvil with diverse activities...")

	for range ticker.C {
		go func() {
			acc := accounts[rand.Intn(len(accounts))]
			acc.Mu.Lock()
			defer acc.Mu.Unlock()

			ctx := context.Background()
			gasPrice, _ := client.SuggestGasPrice(ctx)

			action := rand.Intn(5)
			var tx *types.Transaction

			switch action {
			case 0: // ⛽ Native ETH
				tx = types.NewTransaction(acc.Nonce, accounts[rand.Intn(len(accounts))].Address, big.NewInt(1e15), 21000, gasPrice, nil)
			case 1: // 💸 Transfer
				data := common.FromHex("0xa9059cbb00000000000000000000000070997970c51812dc3a010c7d01b50e0d17dc79ee0000000000000000000000000000000000000000000000000000000000000001")
				tx = types.NewTransaction(acc.Nonce, common.HexToAddress("0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48"), big.NewInt(0), 100000, gasPrice, data)
			case 2: // 🔓 Approve
				data := common.FromHex("0x095ea7b30000000000000000000000003c44cdddb6a900fa2b585dd299e03d12fa4293bcffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff")
				tx = types.NewTransaction(acc.Nonce, common.HexToAddress("0xdAC17F958D2ee523a2206206994597C13D831ec7"), big.NewInt(0), 100000, gasPrice, data)
			case 3: // 🏗️ Deploy
				data := common.FromHex("6080604052348015600f57600080fd5b50603e80601d6000396000f3fe6080604052600080fdfea2646970667358221220ec73696c6c7920636f6e7472616374206279206169206167656e7464736f6c63430008120033")
				tx = types.NewContractCreation(acc.Nonce, big.NewInt(0), 500000, gasPrice, data)
			case 4: // 🍔 Gas Guzzler (Complex Data)
				data := make([]byte, 2000)
				rand.Read(data)
				tx = types.NewTransaction(acc.Nonce, accounts[rand.Intn(len(accounts))].Address, big.NewInt(0), 1000000, gasPrice, data)
			}

			signedTx, _ := types.SignTx(tx, types.LatestSignerForChainID(chainID), acc.Key)
			if err := client.SendTransaction(ctx, signedTx); err == nil {
				acc.Nonce++
			}

			// ⚡ 立即挖矿确保零延迟显示
			_ = client.Client().CallContext(ctx, nil, "anvil_mine", "0x1")
		}()
	}
}
