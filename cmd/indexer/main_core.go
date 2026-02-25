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
	slog.Info("🏁 System Operational.")
	<-sigCh
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
