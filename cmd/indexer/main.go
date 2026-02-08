package main

import (
	"context"
	"log"
	"math/big"
	"os"
	"os/signal"
	"syscall"
	"time"

	"web3-indexer-go/internal/config"
	"web3-indexer-go/internal/database"
	"web3-indexer-go/internal/engine"
)

func main() {
	log.Println("Starting Web3 Indexer...")

	// 1. 加载配置
	cfg := config.Load()
	log.Printf("Config loaded: ChainID=%d, RPC=%s", cfg.ChainID, cfg.RPCURL)

	// 2. 连接数据库
	repo, err := database.NewRepository(cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer repo.Close()
	log.Println("Database connected")

	// 3. 创建Engine
	eng, err := engine.NewEngine(cfg.RPCURL, repo, cfg.ChainID)
	if err != nil {
		log.Fatalf("Failed to create engine: %v", err)
	}
	defer eng.Close()
	log.Println("Engine initialized")

	// 4. 确定起始区块
	ctx := context.Background()
	startBlock, err := repo.GetLatestBlockNumber(ctx, cfg.ChainID)
	if err != nil {
		log.Fatalf("Failed to get latest block: %v", err)
	}

	if cfg.StartBlock > 0 && startBlock.Int64() == 0 {
		startBlock = big.NewInt(cfg.StartBlock)
		log.Printf("Using configured start block: %s", startBlock.String())
	} else if startBlock.Int64() > 0 {
		log.Printf("Resuming from block: %s", startBlock.String())
	} else {
		log.Println("Starting from block 0 (genesis)")
	}

	// 5. 运行同步
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// 处理系统信号
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigChan
		log.Println("Shutdown signal received, stopping...")
		cancel()
	}()

	// 启动同步循环
	if err := eng.Run(ctx, startBlock, 5*time.Second); err != nil {
		log.Fatalf("Indexer stopped with error: %v", err)
	}

	log.Println("Indexer stopped gracefully")
}
