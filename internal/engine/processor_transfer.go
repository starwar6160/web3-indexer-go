package engine

import (
	"math/big"
	"strings"
	"web3-indexer-go/internal/models"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

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

	return &models.Transfer{
		BlockNumber:  models.BigInt{Int: new(big.Int).SetUint64(vLog.BlockNumber)},
		TxHash:       vLog.TxHash.Hex(),
		LogIndex:     uint(vLog.Index),
		From:         strings.ToLower(from.Hex()),
		To:           strings.ToLower(to.Hex()),
		Amount:       amount,
		TokenAddress: strings.ToLower(vLog.Address.Hex()),
	}
}