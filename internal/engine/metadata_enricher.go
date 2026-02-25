package engine

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"web3-indexer-go/internal/models"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
)

// Multicall3Address 全链通用地址
var Multicall3Address = common.HexToAddress("0xca11bde05977b3631167028862be2a173976ca11")

const (
	erc20ABIJSON = `[{"constant":true,"inputs":[],"name":"symbol","outputs":[{"name":"","type":"string"}],"type":"function"},{"constant":true,"inputs":[],"name":"decimals","outputs":[{"name":"","type":"uint8"}],"type":"function"}]`
	multiABIJSON = `[{"inputs":[{"components":[{"internalType":"address","name":"target","type":"address"},{"internalType":"bool","name":"allowFailure","type":"bool"},{"internalType":"bytes","name":"callData","type":"bytes"}],"internalType":"struct Multicall3.Call3[]","name":"calls","type":"tuple[]"}],"name":"aggregate3","outputs":[{"components":[{"internalType":"bool","name":"success","type":"bool"},{"internalType":"bytes","name":"returnData","type":"bytes"}],"internalType":"struct Multicall3.Result[]","name":"returnData","type":"tuple[]"}],"stateMutability":"view","type":"function"}]`
	unknownValue = "UNKNOWN"
)

// DBUpdater 定义数据库更新接口（解耦依赖）
type DBUpdater interface {
	UpdateTokenSymbol(tokenAddress, symbol string) error
	UpdateTokenDecimals(tokenAddress string, decimals uint8) error
	SaveTokenMetadata(meta models.TokenMetadata, address string) error
	LoadAllMetadata() (map[string]models.TokenMetadata, error)
	GetMaxStoredBlock(ctx context.Context) (int64, error)
	GetSyncCursor(ctx context.Context) (int64, error)
	PruneFutureData(ctx context.Context, chainHead int64) error
	UpdateSyncCursor(ctx context.Context, height int64) error
}

// MetadataEnricher 异步元数据丰富器
// 用于在 Sepolia 等真实网络上动态抓取 ERC20 代币的 Symbol 和 Decimals
type MetadataEnricher struct {
	client       LowLevelRPCClient
	cache        sync.Map // addr.Hex() -> models.TokenMetadata
	queue        chan common.Address
	inflight     sync.Map // addr.Hex() -> bool (正在处理中的地址)
	db           DBUpdater
	ctx          context.Context
	cancel       context.CancelFunc
	logger       *slog.Logger
	batchSize     int
	timeout       time.Duration
	batchInterval time.Duration
	erc20ABI      abi.ABI
	multicallABI  abi.ABI
}

// mustParseABI 辅助函数
func mustParseABI(json string) abi.ABI {
	parsed, err := abi.JSON(strings.NewReader(json))
	if err != nil {
		panic(fmt.Sprintf("failed to parse ABI: %v", err))
	}
	return parsed
}

// NewMetadataEnricher 创建元数据丰富器
func NewMetadataEnricher(client LowLevelRPCClient, db DBUpdater, logger *slog.Logger, queueCap int, batchInterval time.Duration) *MetadataEnricher {
	if logger == nil {
		logger = slog.Default()
	}

	if queueCap <= 0 {
		queueCap = 1000
	}
	if batchInterval <= 0 {
		batchInterval = 200 * time.Millisecond
	}

	me := &MetadataEnricher{
		client:        client,
		queue:         make(chan common.Address, queueCap),
		db:            db,
		logger:        logger,
		batchSize:     50, // 提升至 50，进一步利用 Multicall3 带宽
		timeout:       10 * time.Second,
		batchInterval: batchInterval,
		erc20ABI:      mustParseABI(erc20ABIJSON),
		multicallABI:  mustParseABI(multiABIJSON),
	}

	me.ctx, me.cancel = context.WithCancel(context.Background())

	// 🚀 L2 加载：启动时从数据库恢复缓存
	if db != nil {
		if metas, err := db.LoadAllMetadata(); err == nil {
			for addr, m := range metas {
				me.cache.Store(addr, m)
			}
			me.logger.Info("📚 [MetadataEnricher] L2 Cache loaded", "count", len(metas))
		}
	}

	// 启动后台 Worker (移除旧的单条 worker，全量采用批处理以节省配额)
	go me.batchWorker()

	logger.Info("🔍 [MetadataEnricher] Multicall3-enabled worker started", "batch_size", me.batchSize)
	return me
}

// GetSymbol 获取代币符号（带缓存）
func (me *MetadataEnricher) GetSymbol(addr common.Address) string {
	// 零地址检查
	if addr == (common.Address{}) {
		return "ETH"
	}

	addrHex := addr.Hex()

	// 1. 检查 L1 缓存 (Memory)
	if val, ok := me.cache.Load(addrHex); ok {
		if meta, ok := val.(models.TokenMetadata); ok {
			return meta.Symbol
		}
	}

	// 2. 异步入队（带去重，防止重复 RPC）
	if _, loading := me.inflight.LoadOrStore(addrHex, true); !loading {
		select {
		case me.queue <- addr:
			me.logger.Debug("📋 [MetadataEnricher] queued", "address", addrHex)
		default:
			// 🔴 Critical Fix: 队列满时的正确处理
			// 问题: 如果删除 inflight，下次请求会重新入队，导致无限循环
			// 解决: 保持 inflight，但立即删除队列中的一个元素（最老的）并重试
			// 这样保证:
			// 1. 不会无限循环（inflight 阻止重复入队）
			// 2. 队列不会永久满（强制淘汰最老请求）
			// 3. 新请求有公平机会入队
			me.logger.Warn("⚠️ [MetadataEnricher] queue full, evicting oldest request", "address", addrHex)

			// 强制出队一个元素（FIFO 淘汰）
			select {
			case <-me.queue:
				// 成功出队，现在可以入队新请求
				me.queue <- addr
				me.logger.Debug("📋 [MetadataEnricher] queued after eviction", "address", addrHex)
			default:
				// 不应该发生（因为我们刚检测到队列满）
				me.logger.Error("❌ [MetadataEnricher] queue state inconsistent", "address", addrHex)
			}
		}
	}

	// 3. 返回截断的地址作为临时显示
	return addrHex[:10] + "..."
}

// GetDecimals 获取代币精度（带缓存）
func (me *MetadataEnricher) GetDecimals(addr common.Address) uint8 {
	if addr == (common.Address{}) {
		return 18
	}

	addrHex := addr.Hex()
	if val, ok := me.cache.Load(addrHex); ok {
		if meta, ok := val.(models.TokenMetadata); ok {
			return meta.Decimals
		}
	}
	return 18 // 默认 18
}

// batchWorker 批量处理协程（优化 RPC 调用）
func (me *MetadataEnricher) batchWorker() {
	batch := make([]common.Address, 0, me.batchSize)
	ticker := time.NewTicker(me.batchInterval) // 缩短至 200ms，快速清空队列
	defer ticker.Stop()

	for {
		select {
		case <-me.ctx.Done():
			me.logger.Info("🛑 [MetadataEnricher] batch worker stopped")
			return
		case <-ticker.C:
			// 收集一批地址
		collectLoop:
			for len(batch) < me.batchSize {
				select {
				case addr := <-me.queue:
					batch = append(batch, addr)
				default:
					// 队列为空，退出收集循环
					break collectLoop
				}
			}

			if len(batch) > 0 {
				me.processBatch(batch)
				batch = batch[:0] // 清空
			}
		}
	}
}

// processBatch 批量处理（使用 Multicall3 优化）
func (me *MetadataEnricher) processBatch(addresses []common.Address) {
	startTime := time.Now()
	addrCount := len(addresses)

	// 1. 构造 Multicall 调用列表 (每个地址请求 Symbol 和 Decimals)
	// 使用 struct 匹配 Multicall3 Result ABI
	type Call3 struct {
		Target       common.Address
		AllowFailure bool
		CallData     []byte
	}
	calls := make([]Call3, 0, addrCount*2)

	for _, addr := range addresses {
		symData, err := me.erc20ABI.Pack("symbol")
		if err != nil {
			continue
		}
		decData, err := me.erc20ABI.Pack("decimals")
		if err != nil {
			continue
		}
		calls = append(calls,
			Call3{addr, true, symData},
			Call3{addr, true, decData},
		)
	}

	// 2. 打包并发送请求
	input, err := me.multicallABI.Pack("aggregate3", calls)
	if err != nil {
		me.logger.Error("❌ [MetadataEnricher] Pack failed", "err", err)
		return
	}

	ctx, cancel := context.WithTimeout(me.ctx, me.timeout)
	defer cancel()

	msg := ethereum.CallMsg{To: &Multicall3Address, Data: input}
	output, err := me.client.CallContract(ctx, msg, nil)
	if err != nil {
		me.logger.Warn("⚠️ [MetadataEnricher] Multicall3 execution failed", "err", err)
		return
	}

	// 3. 解析结果
	type MultiResult struct {
		Success    bool
		ReturnData []byte
	}
	var multiRes []MultiResult
	if err := me.multicallABI.UnpackIntoInterface(&multiRes, "aggregate3", output); err != nil {
		me.logger.Error("❌ [MetadataEnricher] Unpack failed", "err", err)
		return
	}

	// 4. 对齐结果并分发更新
	for i, addr := range addresses {
		addrHex := addr.Hex()
		meta := models.TokenMetadata{Symbol: "UNKNOWN", Decimals: 18}
		found := false

		// 解析 Symbol (结果索引为 i*2)
		if multiRes[i*2].Success && len(multiRes[i*2].ReturnData) >= 64 {
			// ERC20 symbol 返回 string，需要 Unpack
			if out, err := me.erc20ABI.Unpack("symbol", multiRes[i*2].ReturnData); err == nil && len(out) > 0 {
				if s, ok := out[0].(string); ok {
					meta.Symbol = s
					found = true
				}
			}
		}

		// 解析 Decimals (结果索引为 i*2+1)
		if multiRes[i*2+1].Success && len(multiRes[i*2+1].ReturnData) >= 32 {
			// decimals 返回 uint8
			if out, err := me.erc20ABI.Unpack("decimals", multiRes[i*2+1].ReturnData); err == nil && len(out) > 0 {
				if d, ok := out[0].(uint8); ok {
					meta.Decimals = d
					found = true
				}
			}
		}

		if found {
			// 更新 L1 缓存 (Memory)
			me.cache.Store(addrHex, meta)

			// 🚀 工业级故障隔离：持久化到 L2 (DB) 采用“尽力而为”模式
			// 即使数据库表不存在或写入失败，也不应导致整个同步逻辑回滚
			if me.db != nil {
				if err := me.db.SaveTokenMetadata(meta, addrHex); err != nil {
					me.logger.Warn("⚠️ [MetadataEnricher] L2 persistence failed (non-blocking)",
						"address", addrHex[:10],
						"err", err)
				}
			}

			me.logger.Debug("🎯 [MetadataEnricher] discovered",
				"address", addrHex[:10],
				"symbol", meta.Symbol,
				"decimals", meta.Decimals)
		}

		// 任务完成，移除 inflight 标记
		me.inflight.Delete(addrHex)
	}

	me.logger.Debug("📦 [MetadataEnricher] batch processed",
		"addr_count", addrCount,
		"duration", time.Since(startTime))
}

// Stop 停止丰富器
func (me *MetadataEnricher) Stop() {
	me.cancel()
	me.logger.Info("🛑 [MetadataEnricher] stopped")
}

// GetCacheStats 获取缓存统计（用于监控）
func (me *MetadataEnricher) GetCacheStats() (count int) {
	me.cache.Range(func(_, _ interface{}) bool {
		count++
		return true
	})
	return
}
