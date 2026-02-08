package main

import (
	"context"
	"log"
	"math/big"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"web3-indexer-go/internal/engine"

	_ "github.com/jackc/pgx/v5/stdlib" // PGX Driver
	"github.com/jmoiron/sqlx"
	"github.com/joho/godotenv"
)

func main() {
	log.Println("Starting Web3 Indexer V2 - Production Ready")
	
	// 1. 加载配置
	_ = godotenv.Load()
	rpcUrls := os.Getenv("RPC_URLS")
	dbUrl := os.Getenv("DATABASE_URL")
	
	if rpcUrls == "" || dbUrl == "" {
		log.Fatal("RPC_URLS and DATABASE_URL must be set in environment")
	}

	// 2. 连接资源
	db, err := sqlx.Connect("pgx", dbUrl)
	if err != nil {
		log.Fatalf("DB Connect Error: %v", err)
	}
	defer db.Close()
	
	// 配置数据库连接池
	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(10)
	log.Println("Database connected with connection pool configured")
	
	// 初始化多节点RPC池
	rpcPool, err := engine.NewRPCClientPool(strings.Split(rpcUrls, ","))
	if err != nil {
		log.Fatalf("RPC Pool Error: %v", err)
	}
	defer rpcPool.Close()
	log.Printf("RPC Pool initialized with %d healthy nodes", rpcPool.GetHealthyNodeCount())

	// 3. 初始化组件
	fetcher := engine.NewFetcher(rpcPool, 10) // 10 workers, 100 rps limit
	processor := engine.NewProcessor(db, rpcPool) // 传入RPC池用于reorg恢复
	
	// 致命错误通道 - 用于触发优雅关闭
	fatalErrCh := make(chan error, 1)

	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup

	// 4. 启动 Fetcher
	fetcher.Start(ctx, &wg)

	// 5. 调度任务 (演示模式：抓取 100 个块)
	// 实际生产环境应从数据库 checkpoint 恢复
	startBlock := big.NewInt(18000000)
	endBlock := big.NewInt(18000100)
	fetcher.Schedule(startBlock, endBlock)
	log.Printf("Scheduled blocks %s to %s", startBlock.String(), endBlock.String())

	// 6. 启动 Sequencer - 确保顺序处理
	sequencer := engine.NewSequencer(processor, startBlock, 1, fetcher.Results, fatalErrCh)
	wg.Add(1)
	go func() {
		defer wg.Done()
		sequencer.Run(ctx)
	}()
	
	log.Println("Sequencer started with ordered processing guarantee")

	// 7. 优雅退出处理
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	
	select {
	case sig := <-sigCh:
		log.Printf("Received signal: %v, initiating shutdown...", sig)
	case fatalErr := <-fatalErrCh:
		log.Printf("Fatal error from sequencer: %v, initiating shutdown...", fatalErr)
	}
	
	// 触发优雅关闭
	cancel()
	
	// 停止 Fetcher 以清空任务队列
	fetcher.Stop()
	
	// 等待所有 goroutine 完成
	wg.Wait()
	log.Println("Shutdown complete.")
}
