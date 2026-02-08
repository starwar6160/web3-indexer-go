package main

import (
	"context"
	"log"
	"math/big"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"web3-indexer-go/internal/engine"

	"github.com/ethereum/go-ethereum/ethclient"
	_ "github.com/jackc/pgx/v5/stdlib" // PGX Driver
	"github.com/jmoiron/sqlx"
	"github.com/joho/godotenv"
)

func main() {
	// 1. 加载配置
	_ = godotenv.Load()
	rpcUrl := os.Getenv("RPC_URL")
	dbUrl := os.Getenv("DATABASE_URL")

	// 2. 连接资源
	db, err := sqlx.Connect("pgx", dbUrl)
	if err != nil {
		log.Fatalf("DB Connect Error: %v", err)
	}
	
	client, err := ethclient.Dial(rpcUrl)
	if err != nil {
		log.Fatalf("RPC Connect Error: %v", err)
	}

	// 3. 初始化组件
	fetcher := engine.NewFetcher(client, 10) // 10 workers
	processor := engine.NewProcessor(db)

	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup

	// 4. 启动 Fetcher
	fetcher.Start(ctx, &wg)

	// 5. 调度任务 (这里简单演示，抓取 1000 个块)
	// 实际应先从 DB 读取 Checkpoint
	startBlock := big.NewInt(18000000)
	endBlock := big.NewInt(18000100)
	fetcher.Schedule(startBlock, endBlock)

	// 6. 主循环：消费数据并处理
	// 注意：这里的简单消费无法保证顺序，生产环境需要一个 PriorityQueue 或 Buffer
	// 来保证 Processor 总是按 Block Number 递增处理
	go func() {
		// ⚠️ 极其重要：Go 的 channel 是无序的并发
		// 实际项目中，你需要在这个 loop 里把 result 放入一个 map[uint64]BlockData
		// 然后只有当 map 中存在 expectedBlockNumber 时オ交给 processor 处理
		for data := range fetcher.Results {
			if err := processor.ProcessBlock(data); err != nil {
				log.Printf("Process error: %v", err)
			}
		}
	}()

	// 7. 优雅退出
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh
	
	log.Println("Shutting down...")
	cancel()
	wg.Wait()
	log.Println("Shutdown complete.")
}
