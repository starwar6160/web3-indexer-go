package engine

import (
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

// ProcessLog 从区块日志中提取并识别各种活动（Transfer, Swap, Mint, etc.）
func (p *Processor) ProcessLog(vLog types.Log) *models.Transfer {
	if len(vLog.Topics) == 0 {
		return nil
	}

	var activityType string
	from := ""
	to := ""
	var amount models.Uint256

	switch vLog.Topics[0] {
	case TransferEventHash:
		activityType = "TRANSFER"
		if len(vLog.Topics) >= 3 {
			from = common.BytesToAddress(vLog.Topics[1].Bytes()).Hex()
			to = common.BytesToAddress(vLog.Topics[2].Bytes()).Hex()
		}
		amount = models.NewUint256FromBigInt(new(big.Int).SetBytes(vLog.Data))

	case SwapEventHash:
		activityType = "SWAP"
		if len(vLog.Topics) >= 3 {
			from = common.BytesToAddress(vLog.Topics[1].Bytes()).Hex()
			to = common.BytesToAddress(vLog.Topics[2].Bytes()).Hex()
		}
		amount = models.NewUint256FromBigInt(new(big.Int).SetBytes(vLog.Data))

	case ApprovalEventHash:
		activityType = "APPROVE"
		if len(vLog.Topics) >= 3 {
			from = common.BytesToAddress(vLog.Topics[1].Bytes()).Hex()
			to = common.BytesToAddress(vLog.Topics[2].Bytes()).Hex()
		}
		amount = models.NewUint256FromBigInt(new(big.Int).SetBytes(vLog.Data))

	case MintEventHash:
		activityType = "MINT"
		from = "0x0000000000000000000000000000000000000000"
		if len(vLog.Topics) >= 2 {
			to = common.BytesToAddress(vLog.Topics[1].Bytes()).Hex()
		}
		amount = models.NewUint256FromBigInt(new(big.Int).SetBytes(vLog.Data))

	default:
		// 🚀 记录为通用合约交互
		activityType = "CONTRACT_EVENT"
		from = vLog.Address.Hex()
		to = "Multiple"
		amount = models.NewUint256(0)
	}

	activity := &models.Transfer{
		BlockNumber:  models.BigInt{Int: new(big.Int).SetUint64(vLog.BlockNumber)},
		TxHash:       vLog.TxHash.Hex(),
		LogIndex:     vLog.Index,
		From:         strings.ToLower(from),
		To:           strings.ToLower(to),
		Amount:       amount,
		TokenAddress: strings.ToLower(vLog.Address.Hex()),
		Type:         activityType,
	}

	// 🚀 核心：识别已知实体（如领水）
	fromLabel := GetAddressLabel(activity.From)
	if fromLabel != "" {
		activity.Symbol = fromLabel
		activity.Type = "FAUCET_CLAIM"
	}

	// 🎨 元数据解析逻辑
	staticSymbol := getTokenSymbol(vLog.Address)
	if activity.Symbol == "" {
		if staticSymbol != "" {
			activity.Symbol = staticSymbol
		} else if p.enricher != nil {
			tokenAddr := common.HexToAddress(activity.TokenAddress)
			activity.Symbol = p.enricher.GetSymbol(tokenAddr)
		}
	}

	if activity.Symbol == "" {
		// 对于普通事件，显示合约缩写
		activity.Symbol = activity.TokenAddress[:10] + "..."
	}

	return activity
}

// ProcessTransaction 扫描原始交易以发现部署或原生 ETH 转账
func (p *Processor) ProcessTransaction(_ *big.Int, _ types.Transactions, _ int64) []models.Transfer {
	activities := []models.Transfer{}

	// 这里目前在 ProcessorBatch 或 ProcessBlock 中直接处理了
	// 未来可以抽离到这里进行更复杂的嗅探
	return activities
}
