package emulator

import (
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum/common"
)

// SetBlockInterval 设置触发新区块的间隔
func (e *Emulator) SetBlockInterval(interval time.Duration) {
	e.blockInterval = interval
}

// SetTxInterval 设置发送交易的间隔
func (e *Emulator) SetTxInterval(interval time.Duration) {
	e.txInterval = interval
}

// SetTxAmount 设置每笔转账的金额
func (e *Emulator) SetTxAmount(amount *big.Int) {
	e.txAmount = amount
}

// GetContractAddress 返回部署的合约地址
func (e *Emulator) GetContractAddress() common.Address {
	return e.contract
}

// SetSecurityConfig 设置安全保护参数
func (e *Emulator) SetSecurityConfig(maxGasPrice int64, margin int) {
	e.maxGasPrice = maxGasPrice
	e.gasSafetyMargin = margin
}