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
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
)

// Multicall3Address 全链通用地址
var Multicall3Address = common.HexToAddress("0xca11bde05977b3631167028862be2a173976ca11")

const (
	erc20ABIJSON = `[{"constant":true,"inputs":[],"name":"symbol","outputs":[{"name":"","type":"string"}],"type":"function"},{"constant":true,"inputs":[],"name":"decimals","outputs":[{"name":"","type":"uint8"}],"type":"function"}]`
	multiABIJSON = `[{"inputs":[{"components":[{"internalType":"address","name":"target","type":"address"},{"internalType":"bool","name":"allowFailure","type":"bool"},{"internalType":"bytes","name":"callData","type":"bytes"}],"internalType":"struct Multicall3.Call3[]","name":"calls","type":"tuple[]"}],"name":"aggregate3","outputs":[{"components":[{"internalType":"bool","name":"success","type":"bool"},{"internalType":"bytes","name":"returnData","type":"bytes"}],"internalType":"struct Multicall3.Result[]","name":"returnData","type":"tuple[]"}],"stateMutability":"view","type":"function"}]`
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
	client       LowLevelRPCClient
	cache        sync.Map // addr.Hex() -> TokenMetadata
	queue        chan common.Address
	db           DBUpdater
	ctx          context.Context
	cancel       context.CancelFunc
	logger       *slog.Logger
	batchSize    int
	erc20ABI     abi.ABI
	multicallABI abi.ABI
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

	parsedERC20, _ := abi.JSON(strings.NewReader(erc20ABIJSON))
	parsedMulti, _ := abi.JSON(strings.NewReader(multiABIJSON))

	me := &MetadataEnricher{
		client:       client,
		queue:        make(chan common.Address, 1000), // 增加缓冲区
		db:           db,
		logger:       logger,
		batchSize:    25, // 每次处理 25 个地址，每个地址 2 个调用，共 50 个 call
		erc20ABI:     parsedERC20,
		multicallABI: parsedMulti,
	}

	me.ctx, me.cancel = context.WithCancel(context.Background())

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
		symData, _ := me.erc20ABI.Pack("symbol")
		decData, _ := me.erc20ABI.Pack("decimals")
		calls = append(calls, Call3{addr, true, symData})
		calls = append(calls, Call3{addr, true, decData})
	}

	// 2. 打包并发送请求
	input, err := me.multicallABI.Pack("aggregate3", calls)
	if err != nil {
		me.logger.Error("❌ [MetadataEnricher] Pack failed", "err", err)
		return
	}

	ctx, cancel := context.WithTimeout(me.ctx, 10*time.Second)
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
		meta := TokenMetadata{Symbol: "UNKNOWN", Decimals: 18}
		found := false

		// 解析 Symbol (结果索引为 i*2)
		if multiRes[i*2].Success && len(multiRes[i*2].ReturnData) >= 64 {
			// ERC20 symbol 返回 string，需要 Unpack
			if out, err := me.erc20ABI.Unpack("symbol", multiRes[i*2].ReturnData); err == nil {
				meta.Symbol = out[0].(string)
				found = true
			}
		}

		// 解析 Decimals (结果索引为 i*2+1)
		if multiRes[i*2+1].Success && len(multiRes[i*2+1].ReturnData) >= 32 {
			// decimals 返回 uint8
			if out, err := me.erc20ABI.Unpack("decimals", multiRes[i*2+1].ReturnData); err == nil {
				meta.Decimals = out[0].(uint8)
				found = true
			}
		}

		if found {
			// 更新缓存与 DB
			me.cache.Store(addrHex, meta)
			if me.db != nil {
				_ = me.db.UpdateTokenSymbol(addrHex, meta.Symbol)
				_ = me.db.UpdateTokenDecimals(addrHex, meta.Decimals)
			}
			me.logger.Info("🎯 [MetadataEnricher] discovered",
				"address", addrHex[:10],
				"symbol", meta.Symbol,
				"decimals", meta.Decimals)
		}
	}

	me.logger.Debug("📦 [MetadataEnricher] batch processed",
		"addr_count", addrCount,
		"duration", time.Since(startTime))
}

// fetchTokenMetadata 仍然保留单条查询逻辑作为 Fallback (可选)
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
