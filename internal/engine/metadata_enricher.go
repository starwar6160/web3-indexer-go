package engine

import (
	"context"
	"encoding/hex"
	"log/slog"
	"math/big"
	"strings"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
)

// TokenMetadata 代币元数据结构
type TokenMetadata struct {
	Symbol   string
	Decimals uint8
	Name     string
}

// MetadataEnricher 异步元数据丰富器
// 用于在 Sepolia 等真实网络上动态抓取 ERC20 代币的 Symbol 和 Decimals
type MetadataEnricher struct {
	client    LowLevelRPCClient
	cache     sync.Map // addr.Hex() -> TokenMetadata
	queue     chan common.Address
	db        DBUpdater
	ctx       context.Context
	cancel    context.CancelFunc
	logger    *slog.Logger
	batchSize int
}

// DBUpdater 定义数据库更新接口（解耦依赖）
type DBUpdater interface {
	UpdateTokenSymbol(tokenAddress, symbol string) error
	UpdateTokenDecimals(tokenAddress string, decimals uint8) error
}

// NewMetadataEnricher 创建元数据丰富器
func NewMetadataEnricher(client LowLevelRPCClient, db DBUpdater, logger *slog.Logger) *MetadataEnricher {
	if logger == nil {
		logger = slog.Default()
	}

	me := &MetadataEnricher{
		client:    client,
		queue:     make(chan common.Address, 500), // 缓冲队列
		db:        db,
		logger:    logger,
		batchSize: 20, // 每批处理 20 个地址
	}

	me.ctx, me.cancel = context.WithCancel(context.Background())

	// 启动后台 Worker
	go me.worker()
	go me.batchWorker()

	logger.Info("🔍 [MetadataEnricher] started", "batch_size", me.batchSize)
	return me
}

// GetSymbol 获取代币符号（带缓存）
func (me *MetadataEnricher) GetSymbol(addr common.Address) string {
	// 零地址检查
	if addr == (common.Address{}) {
		return "ETH"
	}

	addrHex := addr.Hex()

	// 1. 检查缓存
	if val, ok := me.cache.Load(addrHex); ok {
		return val.(TokenMetadata).Symbol
	}

	// 2. 异步入队（非阻塞）
	select {
	case me.queue <- addr:
		me.logger.Debug("📋 [MetadataEnricher] queued", "address", addrHex)
	default:
		// 队列满了，跳过（保证不阻塞主进程）
		me.logger.Debug("⚠️ [MetadataEnricher] queue full, skipping", "address", addrHex)
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
		return val.(TokenMetadata).Decimals
	}
	return 18 // 默认 18
}

// worker 单个地址处理协程（用于实时请求）
func (me *MetadataEnricher) worker() {
	for {
		select {
		case <-me.ctx.Done():
			me.logger.Info("🛑 [MetadataEnricher] worker stopped")
			return
		case addr := <-me.queue:
			me.processSingle(addr)
		}
	}
}

// batchWorker 批量处理协程（优化 RPC 调用）
func (me *MetadataEnricher) batchWorker() {
	batch := make([]common.Address, 0, me.batchSize)
	ticker := time.NewTicker(2 * time.Second) // 每 2 秒处理一批
	defer ticker.Stop()

	for {
		select {
		case <-me.ctx.Done():
			me.logger.Info("🛑 [MetadataEnricher] batch worker stopped")
			return
		case <-ticker.C:
			// 收集一批地址
			for len(batch) < me.batchSize {
				select {
				case addr := <-me.queue:
					batch = append(batch, addr)
				default:
					if len(batch) > 0 {
						break
					}
					// 队列为空，等待下一个周期
				}
			}

			if len(batch) > 0 {
				me.processBatch(batch)
				batch = batch[:0] // 清空
			}
		}
	}
}

// processSingle 处理单个地址
func (me *MetadataEnricher) processSingle(addr common.Address) {
	addrHex := addr.Hex()

	// 双重检查（避免重复处理）
	if _, ok := me.cache.Load(addrHex); ok {
		return
	}

	ctx, cancel := context.WithTimeout(me.ctx, 10*time.Second)
	defer cancel()

	metadata, err := me.fetchTokenMetadata(ctx, addr)
	if err != nil {
		me.logger.Debug("⚠️ [MetadataEnricher] fetch failed",
			"address", addrHex,
			"err", err)
		return
	}

	// 更新缓存
	me.cache.Store(addrHex, metadata)
	me.logger.Info("🎯 [MetadataEnricher] discovered",
		"address", addrHex[:10],
		"symbol", metadata.Symbol,
		"decimals", metadata.Decimals)

	// 更新数据库
	if me.db != nil {
		_ = me.db.UpdateTokenSymbol(addrHex, metadata.Symbol)
		_ = me.db.UpdateTokenDecimals(addrHex, metadata.Decimals)
	}
}

// processBatch 批量处理（优化 RPC 调用）
func (me *MetadataEnricher) processBatch(addresses []common.Address) {
	me.logger.Debug("📦 [MetadataEnricher] processing batch", "count", len(addresses))

	for _, addr := range addresses {
		me.processSingle(addr)
		time.Sleep(50 * time.Millisecond) // 避免 RPC 限流
	}
}

// fetchTokenMetadata 从链上抓取代币元数据
func (me *MetadataEnricher) fetchTokenMetadata(ctx context.Context, addr common.Address) (TokenMetadata, error) {
	metadata := TokenMetadata{
		Symbol:   "UNKNOWN",
		Decimals: 18,
		Name:     "Unknown Token",
	}

	// 1. 获取 Symbol
	symbol, err := me.callContractMethod(ctx, addr, "0x95d89b41") // symbol() 的 method ID
	if err == nil && len(symbol) >= 64 {
		metadata.Symbol = me.decodeStringResult(symbol)
	}

	// 2. 获取 Decimals
	decimals, err := me.callContractMethod(ctx, addr, "0x313ce567") // decimals() 的 method ID
	if err == nil && len(decimals) >= 32 {
		d := new(big.Int).SetBytes(common.Hex2Bytes(decimals))
		if d.IsUint64() && d.Uint64() <= 255 {
			metadata.Decimals = uint8(d.Uint64())
		}
	}

	// 3. 获取 Name（可选）
	name, err := me.callContractMethod(ctx, addr, "0x06fdde03") // name() 的 method ID
	if err == nil && len(name) >= 64 {
		metadata.Name = me.decodeStringResult(name)
	}

	return metadata, nil
}

// callContractMethod 调用合约方法（使用 eth_call）
func (me *MetadataEnricher) callContractMethod(ctx context.Context, addr common.Address, methodID string) (string, error) {
	data := common.Hex2Bytes(methodID)
	msg := ethereum.CallMsg{
		To:   &addr,
		Data: data,
	}

	result, err := me.client.CallContract(ctx, msg, nil)
	if err != nil {
		return "", err
	}

	return hex.EncodeToString(result), nil
}

// decodeStringResult 解码 ABI 编码的字符串结果
func (me *MetadataEnricher) decodeStringResult(hexData string) string {
	if len(hexData) < 128 {
		return "UNKNOWN"
	}

	// 跳过 offset (32 bytes) 和 length (32 bytes)
	offset := 64
	lengthHex := hexData[offset : offset+64]
	length := new(big.Int).SetBytes(common.Hex2Bytes(lengthHex)).Int64()

	if length <= 0 || length > 1000 {
		return "UNKNOWN"
	}

	// 读取字符串数据
	dataStart := offset + 64
	dataEnd := dataStart + int(length)*2
	if dataEnd > len(hexData) {
		dataEnd = len(hexData)
	}

	dataHex := hexData[dataStart:dataEnd]
	data, err := hex.DecodeString(dataHex)
	if err != nil {
		return "UNKNOWN"
	}

	// 清理非打印字符
	result := strings.TrimSpace(strings.ToValidUTF8(string(data), ""))
	if len(result) > 50 {
		result = result[:50]
	}

	return result
}

// Stop 停止丰富器
func (me *MetadataEnricher) Stop() {
	me.cancel()
	me.logger.Info("🛑 [MetadataEnricher] stopped")
}

// GetCacheStats 获取缓存统计（用于监控）
func (me *MetadataEnricher) GetCacheStats() (count int) {
	me.cache.Range(func(key, value interface{}) bool {
		count++
		return true
	})
	return
}
