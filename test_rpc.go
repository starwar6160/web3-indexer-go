package main

import (
	"context"
	"fmt"
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum/ethclient"
)

func main() {
	client, err := ethclient.Dial("http://localhost:8545")
	if err != nil {
		panic(err)
	}

	start := time.Now()
	for i := 1000; i < 1100; i++ {
		_, err := client.BlockByNumber(context.Background(), big.NewInt(int64(i)))
		if err != nil {
			fmt.Printf("Error at %d: %v\n", i, err)
		}
	}
	fmt.Printf("100 blocks took %v\n", time.Since(start))
}