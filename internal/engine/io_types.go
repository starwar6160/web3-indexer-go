package engine

import (
	"context"
	"math/big"
	"web3-indexer-go/internal/models"
)

// BlockSource 数据的生产者接口 (RPC 节点, 本地 JSONL 回放等)
type BlockSource interface {
	// FetchLogs 获取指定范围的原始日志
	FetchLogs(ctx context.Context, start, end *big.Int) ([]BlockData, error)
	// GetLatestHeight 获取源端的最新高度
	GetLatestHeight(ctx context.Context) (*big.Int, error)
}

// DataSink 数据的消费者接口 (Postgres, 内存热池, 文件录制器)
type DataSink interface {
	// WriteTransfers 写入转账数据
	WriteTransfers(ctx context.Context, transfers []models.Transfer) error
	// WriteBlocks 写入区块元数据
	WriteBlocks(ctx context.Context, blocks []models.Block) error
	// Close 资源回收
	Close() error
}

// MultiSink 工业级分发器 (类似 Unix 的 tee 命令)
type MultiSink struct {
	sinks []DataSink
}

func NewMultiSink(sinks ...DataSink) *MultiSink {
	return &MultiSink{sinks: sinks}
}

func (m *MultiSink) WriteTransfers(ctx context.Context, transfers []models.Transfer) error {
	for _, sink := range m.sinks {
		if err := sink.WriteTransfers(ctx, transfers); err != nil {
			// 仅记录警告，不中断其它 Sink 的分发，实现故障隔离
			Logger.Warn("sink_write_transfer_failed", "err", err)
		}
	}
	return nil
}

func (m *MultiSink) WriteBlocks(ctx context.Context, blocks []models.Block) error {
	for _, sink := range m.sinks {
		if err := sink.WriteBlocks(ctx, blocks); err != nil {
			Logger.Warn("sink_write_block_failed", "err", err)
		}
	}
	return nil
}

func (m *MultiSink) Close() error {
	for _, sink := range m.sinks {
		_ = sink.Close()
	}
	return nil
}
