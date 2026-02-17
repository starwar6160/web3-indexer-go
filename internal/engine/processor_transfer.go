package engine

import (
	"log/slog"
	"math/big"
	"strings"
	"web3-indexer-go/internal/models"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

// getTokenSymbol 从代币地址映射到符号
func getTokenSymbol(tokenAddr common.Address) string {
	// Sepolia 热门代币地址映射 (Instant Coloring Gene)
	tokenMap := map[string]string{
		"0x1c7d4b196cb0c7b01d743fbc6116a902379c7238": "USDC",
		"0xff34b3d4aee8ddcd6f9afffb6fe49bd371b8a357": "DAI",
		"0x7b79995e5f793a07bc00c21412e50ecae098e7f9": "WETH",
		"0xa3382dffca847b84592c05ab05937a1a38623bc":  "UNI",
		"0x4200000000000000000000000000000000000006": "WETH",
		"0x7af963cf6d228e964f296a96f3ad97a1ee1bb303": "LINK",
		"0x0000000000000000000000000000000000000000": "ETH",
	}

	hexAddr := strings.ToLower(tokenAddr.Hex())
	if symbol, ok := tokenMap[hexAddr]; ok {
		return symbol
	}
	return "" // 返回空，触发异步抓取逻辑
}

// ExtractTransfer 从区块日志中提取 ERC20 Transfer 事件
func (p *Processor) ExtractTransfer(vLog types.Log) *models.Transfer {
	// 检查是否为 Transfer 事件 (topic[0])
	if len(vLog.Topics) < 3 || vLog.Topics[0] != TransferEventHash {
		return nil
	}

	from := common.BytesToAddress(vLog.Topics[1].Bytes())
	to := common.BytesToAddress(vLog.Topics[2].Bytes())
	// 使用 uint256 处理金额，保证金融级精度
	amount := models.NewUint256FromBigInt(new(big.Int).SetBytes(vLog.Data))

	transfer := &models.Transfer{
		BlockNumber:  models.BigInt{Int: new(big.Int).SetUint64(vLog.BlockNumber)},
		TxHash:       vLog.TxHash.Hex(),
		LogIndex:     vLog.Index,
		From:         strings.ToLower(from.Hex()),
		To:           strings.ToLower(to.Hex()),
		Amount:       amount,
		TokenAddress: strings.ToLower(vLog.Address.Hex()),
	}

	// 🎨 元数据解析逻辑：优先使用基因映射，其次异步抓取
	staticSymbol := getTokenSymbol(vLog.Address)
	if staticSymbol != "" {
		transfer.Symbol = staticSymbol
	} else if p.enricher != nil {
		tokenAddr := common.HexToAddress(transfer.TokenAddress)
		transfer.Symbol = p.enricher.GetSymbol(tokenAddr)
	}
	
	// 🛡️ 防御性：确保 symbol 不为空（如果基因和 enricher 都没拿到）
	if transfer.Symbol == "" {
		transfer.Symbol = transfer.TokenAddress[:10] + "..."
	}

	// 📊 记录代币转账统计（用于 Prometheus + Grafana）
	tokenSymbol := transfer.Symbol
	amountFloat := float64(amount.Int.Uint64()) / 1e18 // 假设 18 位小数，转换为标准单位
	p.metrics.RecordTokenTransfer(tokenSymbol, amountFloat)

	// 调试日志（可选）
	slog.Debug("transfer_extracted",
		slog.String("token", tokenSymbol),
		slog.String("amount", amount.String()),
		slog.String("from", transfer.From),
		slog.String("to", transfer.To),
	)

	return transfer
}
