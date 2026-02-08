package main

import (
	"context"
	"fmt"
	"log"
	"math/big"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"web3-indexer-go/internal/engine"

	_ "github.com/jackc/pgx/v5/stdlib" // PGX Driver
	"github.com/jmoiron/sqlx"
	"github.com/joho/godotenv"
)

// getStartBlockFromCheckpoint 从数据库获取起始区块号
func getStartBlockFromCheckpoint(ctx context.Context, db *sqlx.DB, chainID int64) (*big.Int, error) {
	var lastSyncedBlock string
	err := db.GetContext(ctx, &lastSyncedBlock, 
		"SELECT last_synced_block FROM sync_checkpoints WHERE chain_id = $1", chainID)
	
	if err != nil {
		if err.Error() == "sql: no rows in result set" {
			// 没有checkpoint，从0开始
			log.Printf("No checkpoint found for chain %d, starting from block 0", chainID)
			return big.NewInt(0), nil
		}
		return nil, err
	}
	
	// 解析区块号
	blockNum, ok := new(big.Int).SetString(lastSyncedBlock, 10)
	if !ok {
		return nil, fmt.Errorf("invalid block number in checkpoint: %s", lastSyncedBlock)
	}
	
	// 从下一个区块开始
	startBlock := new(big.Int).Add(blockNum, big.NewInt(1))
	log.Printf("Resuming from checkpoint: last synced block %s, starting from %s", 
		blockNum.String(), startBlock.String())
	
	return startBlock, nil
}

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
	
	// Initialize metrics
	metrics := engine.GetMetrics()
	metrics.RecordStartTime()
	
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
	
	// Start HTTP server with health checks and metrics
	mux := http.NewServeMux()
	
	// Initialize health server (pass nil for sequencer, will be updated later)
	healthServer := engine.NewHealthServer(db, rpcPool, nil, fetcher)
	healthServer.RegisterRoutes(mux)
	
	// Start Prometheus metrics server
	mux.Handle("/metrics", promhttp.Handler())
	
	go func() {
		log.Printf("📊 HTTP server started on :8080")
		log.Printf("   Health checks: http://localhost:8080/healthz")
		log.Printf("   Metrics: http://localhost:8080/metrics")
		if err := http.ListenAndServe(":8080", mux); err != nil {
			log.Printf("HTTP server error: %v", err)
		}
	}()
	
	// 致命错误通道 - 用于触发优雅关闭
	fatalErrCh := make(chan error, 1)

	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup

	// 4. 启动 Fetcher
	fetcher.Start(ctx, &wg)

	// 5. 从 checkpoint 恢复起始区块
	chainID := int64(1) // TODO: 从环境变量读取
	startBlock, err := getStartBlockFromCheckpoint(ctx, db, chainID)
	if err != nil {
		log.Fatalf("Failed to get start block from checkpoint: %v", err)
	}
	
	// 调度任务 (从 checkpoint 开始同步 100 个块用于演示)
	endBlock := new(big.Int).Add(startBlock, big.NewInt(100))
	fetcher.Schedule(startBlock, endBlock)
	log.Printf("Scheduled blocks %s to %s (resumed from checkpoint)", startBlock.String(), endBlock.String())

	// 6. 启动 Sequencer - 确保顺序处理（传入 Fetcher 用于 Reorg 时暂停）
	sequencer := engine.NewSequencerWithFetcher(processor, fetcher, startBlock, 1, fetcher.Results, fatalErrCh, metrics)
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
