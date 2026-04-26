package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"web3-indexer-go/internal/config"
	"web3-indexer-go/internal/engine"
	"web3-indexer-go/internal/recovery"
	"web3-indexer-go/internal/web"
)

func main() {
	if err := run(); err != nil {
		os.Exit(1)
	}
}

func run() error {
	resetDB := flag.Bool("reset", false, "Reset database")
	startFrom := flag.String("start-from", "", "Force start from: 'latest' or specific block number")
	mode := flag.String("mode", "index", "Operation mode: 'index' or 'replay'")
	replayFile := flag.String("file", "", "Trajectory file for replay (.jsonl or .lz4)")
	replaySpeed := flag.Float64("speed", 1.0, "Replay speed factor (e.g. 2.0 for 2x speed, 0 for max speed)")
	flag.Parse()
	cfg = config.Load()
	engine.InitLogger(cfg.LogLevel)
	forceFrom = *startFrom

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	wsHub := setupWebSocketHub(ctx)

	if *mode == "replay" {
		return startReplayMode(ctx, *replayFile, *replaySpeed)
	}

	apiServer := NewServer(nil, wsHub, cfg.Port, cfg.AppTitle)
	recovery.WithRecovery(func() {
		slog.Info("🚀 Indexer API Server starting (Early Bird Mode)", "port", cfg.Port)
		if err := apiServer.Start(); err != nil && err != http.ErrServerClosed {
			slog.Error("api_start_fail", "err", err)
		}
	}, "api_server")

	recovery.WithRecoveryNamed("async_init", func() {
		initEngine(ctx, apiServer, wsHub, *resetDB)
	})

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	slog.Info("🏁 System Operational. Press Ctrl+C to stop.")
	sig := <-sigCh
	slog.Info("🛑 Signal received, initiating graceful shutdown...", "signal", sig)

	// 1. 创建 15 秒超时 context 用于关闭流程
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer shutdownCancel()

	// 2. 首先关闭 API Server（停止接收新连接）
	if err := apiServer.Shutdown(shutdownCtx); err != nil {
		slog.Error("🛑 API Server shutdown error", "err", err)
	} else {
		slog.Info("✅ API Server shut down gracefully")
	}

	// 3. 取消全局 Context，通知 Sequencer, Fetcher 等组件停止
	cancel()

	// 4. 关闭 Orchestrator（会触发 AsyncWriter 的 Flush 和 Shutdown）
	orchestrator := engine.GetOrchestrator()
	if orchestrator != nil {
		slog.Info("🎼 Shutting down Orchestrator and Flushing DB...")
		orchestrator.Shutdown()
		slog.Info("✅ Orchestrator and AsyncWriter shut down")
	}

	slog.Info("🏁 Graceful shutdown complete. Goodbye!")
	return nil
}

func setupWebSocketHub(ctx context.Context) *web.Hub {
	var wsHub *web.Hub
	if cfg.ChainID == 31337 {
		throttledHub := web.NewThrottledHub(500 * time.Millisecond)
		go throttledHub.RunWithThrottling(ctx)
		wsHub = throttledHub.Hub
		slog.Info("🔥 Throttled WebSocket Hub activated for Anvil", "throttle", "500ms")
	} else {
		wsHub = web.NewHub()
		go wsHub.Run(ctx)
		slog.Info("📡 Standard WebSocket Hub activated for production")
	}
	return wsHub
}

func startReplayMode(ctx context.Context, replayFile string, replaySpeed float64) error {
	if replayFile == "" {
		slog.Error("❌ Replay mode requires -file parameter")
		return fmt.Errorf("replay mode requires -file parameter")
	}
	db, err := connectDB(ctx, cfg.ChainID == 31337)
	if err != nil {
		return err
	}
	processor := engine.NewProcessor(db, nil, 100, cfg.ChainID, false, "replay")
	orchestrator := engine.GetOrchestrator()
	asyncWriter := engine.NewAsyncWriter(db, orchestrator, cfg.EphemeralMode, cfg.ChainID)
	orchestrator.SetAsyncWriter(asyncWriter)
	asyncWriter.Start()

	slog.Info("🏁 System starting in REPLAY mode.")
	return RunReplayMode(ctx, replayFile, replaySpeed, processor)
}
