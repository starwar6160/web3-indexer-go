package engine

import (
	"log/slog"
	"os"
)

// Logger 全局结构化日志器
var Logger *slog.Logger

// InitLogger 初始化结构化日志
func InitLogger(level string) {
	var logLevel slog.Level
	switch level {
	case "debug":
		logLevel = slog.LevelDebug
	case "info":
		logLevel = slog.LevelInfo
	case "warn":
		logLevel = slog.LevelWarn
	case "error":
		logLevel = slog.LevelError
	default:
		logLevel = slog.LevelInfo
	}

	// 创建 JSON 格式的结构化日志器
	opts := &slog.HandlerOptions{
		Level: logLevel,
	}

	// 根据环境变量选择输出格式
	if os.Getenv("LOG_FORMAT") == "text" {
		// 文本格式，便于开发调试
		Logger = slog.New(slog.NewTextHandler(os.Stdout, opts))
	} else {
		// JSON 格式，便于日志收集系统处理
		Logger = slog.New(slog.NewJSONHandler(os.Stdout, opts))
	}

	slog.SetDefault(Logger)
}

// LogBlockProcessing 记录区块处理日志
func LogBlockProcessing(blockNumber, blockHash string, duration float64) {
	Logger.Info("block_processed",
		slog.String("block_number", blockNumber),
		slog.String("block_hash", blockHash),
		slog.Float64("duration_seconds", duration),
	)
}

// LogReorgDetected 记录重组检测日志
func LogReorgDetected(blockNumber, expectedParent, actualParent string) {
	Logger.Warn("reorg_detected",
		slog.String("block_number", blockNumber),
		slog.String("expected_parent", expectedParent),
		slog.String("actual_parent", actualParent),
	)
}

// LogReorgHandled 记录重组处理日志
func LogReorgHandled(blocksAffected int, ancestorBlock string) {
	Logger.Info("reorg_handled",
		slog.Int("blocks_affected", blocksAffected),
		slog.String("ancestor_block", ancestorBlock),
	)
}

// LogFetcherPaused 记录 Fetcher 暂停日志
func LogFetcherPaused(reason string) {
	Logger.Info("fetcher_paused",
		slog.String("reason", reason),
	)
}

// LogFetcherResumed 记录 Fetcher 恢复日志
func LogFetcherResumed() {
	Logger.Info("fetcher_resumed")
}

// LogRPCRetry 记录 RPC 重试日志
func LogRPCRetry(method string, attempt int, err error) {
	Logger.Warn("rpc_retry",
		slog.String("method", method),
		slog.Int("attempt", attempt),
		slog.String("error", err.Error()),
	)
}

// LogCheckpointResumed 记录从 checkpoint 恢复日志
func LogCheckpointResumed(lastSyncedBlock, startBlock string) {
	Logger.Info("checkpoint_resumed",
		slog.String("last_synced_block", lastSyncedBlock),
		slog.String("start_block", startBlock),
	)
}

// LogBufferFull 记录缓冲区满日志
func LogBufferFull(bufferSize int, expectedBlock string) {
	Logger.Error("sequencer_buffer_full",
		slog.Int("buffer_size", bufferSize),
		slog.String("expected_block", expectedBlock),
	)
}

// LogDatabaseError 记录数据库错误日志
func LogDatabaseError(operation string, err error) {
	Logger.Error("database_error",
		slog.String("operation", operation),
		slog.String("error", err.Error()),
	)
}
