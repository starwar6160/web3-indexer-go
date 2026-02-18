package engine

import (
	"fmt"
	"time"

	"github.com/ethereum/go-ethereum/common"
)

func (s *DeFiSimulator) generateDeFiTransfer(seqNum int64) *SynthesizedTransfer {
	s.mu.Lock()
	defer s.mu.Unlock()

	header, err := s.client.HeaderByNumber(s.ctx, nil)
	if err != nil {
		return nil
	}
	currentBlock := header.Number.Uint64()

	txType := secureIntn(100)
	var transfer *SynthesizedTransfer

	switch {
	case txType < 60:
		transfer = s.generateSwapTransfer(currentBlock, seqNum)
	case txType < 80:
		transfer = s.generateArbitrageTransfer(currentBlock, seqNum)
	case txType < 90:
		transfer = s.generateFlashloanTransfer(currentBlock, seqNum)
	default:
		transfer = s.generateMEVTransfer(currentBlock, seqNum)
	}

	return transfer
}

func (s *DeFiSimulator) generateSwapTransfer(blockNumber uint64, seqNum int64) *SynthesizedTransfer {
	token0 := s.tokens[secureIntn(len(s.tokens))]
	amountRaw := s.generatePowerLawAmount(token0.Decimals)
	from := s.randomUserAddress()
	to := s.uniswapV3Router

	return &SynthesizedTransfer{
		TxHash:       s.generateTxHash(blockNumber, seqNum, "SWAP"),
		BlockNumber:  blockNumber,
		BlockHash:    common.HexToHash(fmt.Sprintf("0x%d", blockNumber)),
		TokenAddress: token0.Address,
		From:         from,
		To:           to,
		Amount:       amountRaw,
		Timestamp:    time.Now().Unix(),
		Synthesized:  true,
	}
}

func (s *DeFiSimulator) generateArbitrageTransfer(blockNumber uint64, seqNum int64) *SynthesizedTransfer {
	bot := s.arbitrageBots[secureIntn(len(s.arbitrageBots))]
	token0 := s.tokens[secureIntn(len(s.tokens))]
	amountRaw := s.generateLargeAmount(token0.Decimals)

	return &SynthesizedTransfer{
		TxHash:       s.generateTxHash(blockNumber, seqNum, "ARBITRAGE"),
		BlockNumber:  blockNumber,
		BlockHash:    common.HexToHash(fmt.Sprintf("0x%d", blockNumber)),
		TokenAddress: token0.Address,
		From:         bot,
		To:           s.uniswapV3Router,
		Amount:       amountRaw,
		Timestamp:    time.Now().Unix(),
		Synthesized:  true,
	}
}

func (s *DeFiSimulator) generateFlashloanTransfer(blockNumber uint64, seqNum int64) *SynthesizedTransfer {
	token := s.tokens[secureIntn(len(s.tokens))]
	amountRaw := s.generateMegaAmount(token.Decimals)

	return &SynthesizedTransfer{
		TxHash:       s.generateTxHash(blockNumber, seqNum, "FLASHLOAN"),
		BlockNumber:  blockNumber,
		BlockHash:    common.HexToHash(fmt.Sprintf("0x%d", blockNumber)),
		TokenAddress: token.Address,
		From:         s.aaveV3Pool,
		To:           s.balancerVault,
		Amount:       amountRaw,
		Timestamp:    time.Now().Unix(),
		Synthesized:  true,
	}
}

func (s *DeFiSimulator) generateMEVTransfer(blockNumber uint64, seqNum int64) *SynthesizedTransfer {
	bot := s.arbitrageBots[secureIntn(len(s.arbitrageBots))]
	token := s.tokens[3] // WETH
	amountRaw := s.generateMediumAmount(token.Decimals)

	return &SynthesizedTransfer{
		TxHash:       s.generateTxHash(blockNumber, seqNum, "MEV"),
		BlockNumber:  blockNumber,
		BlockHash:    common.HexToHash(fmt.Sprintf("0x%d", blockNumber)),
		TokenAddress: token.Address,
		From:         bot,
		To:           s.uniswapV3Router,
		Amount:       amountRaw,
		Timestamp:    time.Now().Unix(),
		Synthesized:  true,
	}
}

func (s *DeFiSimulator) generateTxHash(blockNumber uint64, seqNum int64, txType string) common.Hash {
	data := make([]byte, 32)
	// #nosec G115 - Manual byte packing for synthesized hash
	data[0] = byte(blockNumber >> 24)
	// #nosec G115
	data[1] = byte(blockNumber >> 16)
	// #nosec G115
	data[2] = byte(blockNumber >> 8)
	// #nosec G115
	data[3] = byte(blockNumber)
	// #nosec G115
	data[4] = byte(seqNum)
	// #nosec G115
	data[5] = byte(len(txType))
	return common.BytesToHash(data)
}
