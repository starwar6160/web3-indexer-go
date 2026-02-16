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

// SimpleERC20 is a minimal ERC20 contract ABI for testing.
const SimpleERC20ABI = `[
	{"type":"constructor","inputs":[{"name":"initialSupply","type":"uint256"}]},
	{"type":"function","name":"transfer","inputs":[{"name":"to","type":"address"},{"name":"amount","type":"uint256"}],"outputs":[{"name":"","type":"bool"}]},
	{"type":"function","name":"balanceOf","inputs":[{"name":"account","type":"address"}],"outputs":[{"name":"","type":"uint256"}]},
	{"type":"event","name":"Transfer","inputs":[{"name":"from","type":"address","indexed":true},{"name":"to","type":"address","indexed":true},{"name":"value","type":"uint256","indexed":false}]}
]`

// SimpleERC20Bytecode is the compiled bytecode for a minimal ERC20 contract.
const SimpleERC20Bytecode = `608060405234801561001057600080fd5b506040516103b03803806103b083398101604081905261002f91610062565b600080546001600160a01b031916331790556001805461004f8382610099565b50505061012e565b60006020828403121561007457600080fd5b5051919050565b634e487b7160e01b600052604160045260246000fd5b80820180821115610128577f4e487b7160e01b600052601160045260246000fd5b92915050565b6102738061013d6000396000f3fe608060405234801561001057600080fd5b50600436106100415760003560e01c806306fdde031461004657806370a0823114610064578063a9059cbb14610095575b600080fd5b61004e6100c6565b60405161005b9190610154565b60405180910390f35b61007e6100723660046101a0565b60016020526000908152604090205481565b60405190815260200161005b565b6100b86100a33660046101c2565b6100d4565b604051901515815260200161005b565b60606040518060400160405280600381526020016245524360e01b815250905090565b6000336001600160a01b0316600160008373ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff168152602001908152602001600020541061014a57600080fd5b600160008373ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff16815260200190815260200160002054600160008573ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff168152602001908152602001600020600082825461019e91906101e4565b9091555050600192915050565b6000602082840312156101b257600080fd5b81356001600160a01b03811681146101c957600080fd5b9392505050565b600080604083850312156101e357600080fd5b50508035906020909101359150565b8082018082111561020a577f4e487b7160e01b600052601160045260246000fd5b9291505056fea26469706673582212204d5a1b5a5a5a5a5a5a5a5a5a5a5a5a5a5a5a5a5a5a5a5a5a5a5a5a5a5a5a5a64736f6c63430008140033`

func main() {
	rpcURL := os.Getenv("RPC_URL")
	if rpcURL == "" {
		rpcURL = "http://localhost:8545"
	}

	client, err := ethclient.Dial(rpcURL)
	if err != nil {
		log.Fatalf("Failed to connect to Anvil: %v", err)
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Verify connection
	chainID, err := client.ChainID(ctx)
	if err != nil {
		log.Printf("Failed to get chain ID: %v", err)
		return
	}
	fmt.Printf("✅ Connected to Anvil (Chain ID: %d)\n", chainID)

	// Get the first Ganache account (pre-funded)
	// Using Ganache's default mnemonic accounts
	// Account 0: 0x9bb57efaff34f3558f314e6fe06eeb63ce9f3909
	privateKey, err := crypto.HexToECDSA("4f3edf983ac636a65a842ce7c78d9aa706d3b113bce9c46f30d7d21715b23b1d")
	if err != nil {
		log.Printf("Failed to parse private key: %v", err)
		return
	}

	publicKey := privateKey.Public()
	publicKeyECDSA, ok := publicKey.(*ecdsa.PublicKey)
	if !ok {
		log.Printf("Failed to cast public key to ECDSA")
		return
	}

	fromAddress := crypto.PubkeyToAddress(*publicKeyECDSA)
	fmt.Printf("📝 Deploying from: %s\n", fromAddress.Hex())

	// Get nonce
	nonce, err := client.PendingNonceAt(ctx, fromAddress)
	if err != nil {
		log.Fatalf("Failed to get nonce: %v", err)
	}

	// Get gas price
	gasPrice, err := client.SuggestGasPrice(ctx)
	if err != nil {
		log.Fatalf("Failed to get gas price: %v", err)
	}

	// Deploy contract
	fmt.Println("🚀 Deploying ERC20 contract...")
	contractAddress, txHash, err := deployContract(ctx, client, privateKey, nonce, gasPrice, chainID)
	if err != nil {
		log.Fatalf("Failed to deploy contract: %v", err)
	}

	fmt.Printf("✅ Contract deployed at: %s\n", contractAddress.Hex())
	fmt.Printf("📋 Deployment TX: %s\n", txHash.Hex())

	// Wait for deployment confirmation
	time.Sleep(2 * time.Second)

	// Send test transactions
	fmt.Println("\n📤 Sending test transactions...")
	nonce++

	recipientAddress := common.HexToAddress("0x70997970C51812e339D9B73b0245ad59e5E05a77") // Anvil account 2

	for i := 0; i < 10; i++ {
		tx, err := sendTransfer(ctx, client, privateKey, nonce+uint64(i), gasPrice, chainID, contractAddress, recipientAddress)
		if err != nil {
			log.Printf("❌ Failed to send transaction %d: %v", i+1, err)
			continue
		}
		fmt.Printf("✅ TX %d sent: %s\n", i+1, tx.Hex())
		time.Sleep(500 * time.Millisecond)
	}

	fmt.Println("\n✨ Demo deployment complete!")
	fmt.Printf("📊 Contract address: %s\n", contractAddress.Hex())
	fmt.Printf("🎯 You can now start the indexer with:\n")
	fmt.Printf("   RPC_URLS=http://localhost:8545 CHAIN_ID=31337 START_BLOCK=0 ./bin/indexer\n")
}

func deployContract(ctx context.Context, client *ethclient.Client, privateKey *ecdsa.PrivateKey, nonce uint64, gasPrice, chainID *big.Int) (common.Address, common.Hash, error) {
	auth, err := bind.NewKeyedTransactorWithChainID(privateKey, chainID)
	if err != nil {
		return common.Address{}, common.Hash{}, err
	}

	auth.Nonce = new(big.Int).SetUint64(nonce)
	auth.Value = big.NewInt(0)
	auth.GasLimit = uint64(3000000)
	auth.GasPrice = gasPrice

	// Create raw transaction for contract deployment
	tx := types.NewContractCreation(nonce, big.NewInt(0), 3000000, gasPrice, common.FromHex(SimpleERC20Bytecode))

	signedTx, err := auth.Signer(auth.From, tx)
	if err != nil {
		return common.Address{}, common.Hash{}, err
	}

	err = client.SendTransaction(ctx, signedTx)
	if err != nil {
		return common.Address{}, common.Hash{}, err
	}

	// Wait for receipt
	receipt, err := bind.WaitMined(ctx, client, signedTx)
	if err != nil {
		return common.Address{}, common.Hash{}, err
	}

	return receipt.ContractAddress, receipt.TxHash, nil
}

func sendTransfer(ctx context.Context, client *ethclient.Client, privateKey *ecdsa.PrivateKey, nonce uint64, gasPrice, chainID *big.Int, contractAddr, toAddr common.Address) (common.Hash, error) {
	auth, err := bind.NewKeyedTransactorWithChainID(privateKey, chainID)
	if err != nil {
		return common.Hash{}, err
	}

	auth.Nonce = new(big.Int).SetUint64(nonce)
	auth.Value = big.NewInt(0)
	auth.GasLimit = uint64(100000)
	auth.GasPrice = gasPrice

	// Create transfer transaction (simplified)
	// In a real scenario, you'd use contract ABI binding
	data := make([]byte, 0)
	data = append(data, 0xa9, 0x05, 0x9c, 0xbb) // transfer function selector
	data = append(data, toAddr.Bytes()...)
	data = append(data, make([]byte, 32)...)
	data[len(data)-1] = 100 // amount = 100

	tx := types.NewTransaction(nonce, contractAddr, big.NewInt(0), 100000, gasPrice, data)

	signedTx, err := auth.Signer(auth.From, tx)
	if err != nil {
		return common.Hash{}, err
	}

	err = client.SendTransaction(ctx, signedTx)
	if err != nil {
		return common.Hash{}, err
	}

	return signedTx.Hash(), nil
}
