package main

import (
	"context"
	"crypto/ecdsa"
	"fmt"
	"log"
	"math/big"
	"os"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
)

func main() {
	rpcURL := os.Getenv("RPC_URL")
	if rpcURL == "" {
		rpcURL = "http://localhost:8545"
	}

	client, err := ethclient.Dial(rpcURL)
	if err != nil {
		log.Fatalf("Failed to connect to Ganache: %v", err)
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Verify connection
	chainID, err := client.ChainID(ctx)
	if err != nil {
		log.Fatalf("Failed to get chain ID: %v", err)
	}
	fmt.Printf("✅ Connected to Ganache (Chain ID: %d)\n", chainID)

	// Use Anvil's default account 0 (pre-funded with 10000 ETH)
	// This is Anvil's golden private key - always has funds
	privateKeyHex := "ac0974bec39a17e36ba4a6b4d238ff944bacb476c6b8d6c1f02e8a3e7c4d5e6f"
	privateKey, err := crypto.HexToECDSA(privateKeyHex)
	if err != nil {
		log.Fatalf("Failed to parse private key: %v", err)
	}

	publicKey := privateKey.Public()
	publicKeyECDSA, ok := publicKey.(*ecdsa.PublicKey)
	if !ok {
		log.Fatal("Failed to cast public key to ECDSA")
	}

	fromAddress := crypto.PubkeyToAddress(*publicKeyECDSA)
	fmt.Printf("📝 Deploying from: %s\n", fromAddress.Hex())

	// Check balance
	balance, err := client.BalanceAt(ctx, fromAddress, nil)
	if err != nil {
		log.Fatalf("Failed to get balance: %v", err)
	}
	fmt.Printf("💰 Account balance: %s wei\n", balance.String())

	// Get nonce
	nonce, err := client.PendingNonceAt(ctx, fromAddress)
	if err != nil {
		log.Fatalf("Failed to get nonce: %v", err)
	}
	fmt.Printf("🔢 Nonce: %d\n", nonce)

	// Get gas price
	gasPrice, err := client.SuggestGasPrice(ctx)
	if err != nil {
		log.Fatalf("Failed to get gas price: %v", err)
	}
	fmt.Printf("⛽️  Gas price: %s wei\n", gasPrice.String())

	// Create auth
	auth, err := bind.NewKeyedTransactorWithChainID(privateKey, chainID)
	if err != nil {
		log.Fatalf("Failed to create auth: %v", err)
	}

	auth.Nonce = big.NewInt(int64(nonce))
	auth.Value = big.NewInt(0)
	auth.GasLimit = uint64(3000000)
	auth.GasPrice = gasPrice

	// Simple contract bytecode (minimal contract)
	contractBytecode := "608060405234801561001057600080fd5b50610150806100206000396000f3fe608060405234801561001057600080fd5b50600436106100365760003560e01c806306fdde031461003b578063c47f002714610059575b600080fd5b610043610075565b6040516100509190610103565b60405180910390f35b610073600480360381019061006e919061012f565b6100a3565b005b60606040518060400160405280600381526020016245524360e01b815250905090565b8073ffffffffffffffffffffffffffffffffffffffff167f1f6f5b5a5a5a5a5a5a5a5a5a5a5a5a5a5a5a5a5a5a5a5a5a5a5a5a5a5a5a5a60405160405180910390a250565b600081519050919050565b600082825260208201905092915050565b60005b8381101561012a57808201518184015260208101905061010f565b60008484015250505050565b6000602082840312156101485761014761010a565b5b6000610156848285016100e4565b9150509291505056fea26469706673582212204d5a1b5a5a5a5a5a5a5a5a5a5a5a5a5a5a5a5a5a5a5a5a5a5a5a5a5a5a5a5a64736f6c63430008140033"

	fmt.Println("🚀 Deploying contract...")
	tx := types.NewContractCreation(nonce, big.NewInt(0), 3000000, gasPrice, common.FromHex(contractBytecode))

	signedTx, err := auth.Signer(auth.From, tx)
	if err != nil {
		log.Fatalf("Failed to sign transaction: %v", err)
	}

	err = client.SendTransaction(ctx, signedTx)
	if err != nil {
		log.Fatalf("Failed to send transaction: %v", err)
	}

	fmt.Printf("✅ Contract deployment TX sent: %s\n", signedTx.Hash().Hex())

	// Wait for receipt
	receipt, err := bind.WaitMined(ctx, client, signedTx)
	if err != nil {
		log.Fatalf("Failed to wait for mining: %v", err)
	}

	fmt.Printf("✅ Contract deployed at: %s\n", receipt.ContractAddress.Hex())
	fmt.Printf("📋 Block number: %d\n", receipt.BlockNumber)

	// Send test transactions
	fmt.Println("\n📤 Sending test transactions...")
	recipientAddress := common.HexToAddress("0xe61080134449e25757d3a572a04baee01de1ef40")

	for i := 0; i < 5; i++ {
		nonce++
		txData := make([]byte, 0)
		txData = append(txData, 0xc4, 0x7f, 0x00, 0x27) // function selector
		txData = append(txData, recipientAddress.Bytes()...)

		tx := types.NewTransaction(nonce, receipt.ContractAddress, big.NewInt(0), 100000, gasPrice, txData)
		signedTx, err := auth.Signer(auth.From, tx)
		if err != nil {
			log.Printf("❌ Failed to sign TX %d: %v", i+1, err)
			continue
		}

		err = client.SendTransaction(ctx, signedTx)
		if err != nil {
			log.Printf("❌ Failed to send TX %d: %v", i+1, err)
			continue
		}

		fmt.Printf("✅ TX %d sent: %s\n", i+1, signedTx.Hash().Hex())
		time.Sleep(500 * time.Millisecond)
	}

	fmt.Println("\n✨ Demo deployment complete!")
	fmt.Printf("📊 Contract address: %s\n", receipt.ContractAddress.Hex())
	fmt.Println("🎯 Dashboard should now show blocks and transactions!")
}
