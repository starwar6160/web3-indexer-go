package engine

import (
	"context"
	"fmt"
	"log"
	"math/big"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	"web3-indexer-go/internal/database"
	"web3-indexer-go/internal/models"
)

// ERC20 Transfer event signature hash
var TransferEventHash = common.HexToHash("0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef")

// USDT contract address (mainnet)
var USDTAddress = common.HexToAddress("0xdac17f958d2ee523a2206206994597c13d831ec7")

type Engine struct {
	client     *ethclient.Client
	repo       *database.Repository
	chainID    int64
	batchSize  int64
}

func NewEngine(rpcURL string, repo *database.Repository, chainID int64) (*Engine, error) {
	client, err := ethclient.Dial(rpcURL)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to RPC: %w", err)
	}

	return &Engine{
		client:    client,
		repo:      repo,
		chainID:   chainID,
		batchSize: 100, // 每次处理100个区块
	}, nil
}

func (e *Engine) Close() {
	e.client.Close()
}

// Sync 执行从 fromBlock 到 toBlock 的同步
func (e *Engine) Sync(ctx context.Context, fromBlock, toBlock *big.Int) error {
	current := new(big.Int).Set(fromBlock)
	
	for current.Cmp(toBlock) <= 0 {
		endBlock := new(big.Int).Add(current, big.NewInt(e.batchSize-1))
		if endBlock.Cmp(toBlock) > 0 {
			endBlock = toBlock
		}

		log.Printf("Syncing blocks %s to %s", current.String(), endBlock.String())
		
		if err := e.processRange(ctx, current, endBlock); err != nil {
			return fmt.Errorf("failed to process range %s-%s: %w", current.String(), endBlock.String(), err)
		}

		// 更新checkpoint
		if err := e.repo.UpdateCheckpoint(ctx, e.chainID, endBlock); err != nil {
			log.Printf("Warning: failed to update checkpoint: %v", err)
		}

		current.Add(endBlock, big.NewInt(1))
	}

	return nil
}

func (e *Engine) processRange(ctx context.Context, fromBlock, toBlock *big.Int) error {
	// 构建过滤器查询 - 监听所有ERC20 Transfer事件
	query := ethereum.FilterQuery{
		FromBlock: fromBlock,
		ToBlock:   toBlock,
		Topics: [][]common.Hash{
			{TransferEventHash}, // Event signature
		},
	}

	logs, err := e.client.FilterLogs(ctx, query)
	if err != nil {
		return fmt.Errorf("failed to filter logs: %w", err)
	}

	log.Printf("Found %d transfer events", len(logs))

	// 先保存所有区块信息
	blockCache := make(map[uint64]*models.Block)
	
	for _, vLog := range logs {
		blockNum := vLog.BlockNumber
		if _, exists := blockCache[blockNum]; !exists {
			block, err := e.client.BlockByNumber(ctx, new(big.Int).SetUint64(blockNum))
			if err != nil {
				log.Printf("Warning: failed to get block %d: %v", blockNum, err)
				continue
			}
			
			blockCache[blockNum] = &models.Block{
				Number:     models.BigInt{Int: block.Number()},
				Hash:       block.Hash().Hex(),
				ParentHash: block.ParentHash().Hex(),
				Timestamp:  block.Time(),
			}
		}
	}

	// 批量保存区块
	for _, block := range blockCache {
		if err := e.repo.SaveBlock(ctx, block); err != nil {
			log.Printf("Warning: failed to save block %s: %v", block.Number.String(), err)
		}
	}

	// 解析并保存Transfer事件
	for _, vLog := range logs {
		transfer, err := e.parseTransfer(vLog)
		if err != nil {
			log.Printf("Warning: failed to parse transfer: %v", err)
			continue
		}

		if err := e.repo.SaveTransfer(ctx, transfer); err != nil {
			log.Printf("Warning: failed to save transfer: %v", err)
		}
	}

	return nil
}

func (e *Engine) parseTransfer(vLog types.Log) (*models.Transfer, error) {
	// ERC20 Transfer event has 3 indexed topics:
	// 0: Event signature
	// 1: from address (indexed)
	// 2: to address (indexed)
	// Data: amount (32 bytes)

	if len(vLog.Topics) < 3 {
		return nil, fmt.Errorf("invalid log format: insufficient topics")
	}

	from := common.HexToAddress(vLog.Topics[1].Hex())
	to := common.HexToAddress(vLog.Topics[2].Hex())

	// 解析amount (data字段)
	amount := new(big.Int).SetBytes(vLog.Data)

	return &models.Transfer{
		BlockNumber:  models.BigInt{Int: new(big.Int).SetUint64(vLog.BlockNumber)},
		TxHash:       vLog.TxHash.Hex(),
		LogIndex:     uint(vLog.Index),
		From:         strings.ToLower(from.Hex()),
		To:           strings.ToLower(to.Hex()),
		Amount:       models.BigInt{Int: amount},
		TokenAddress: strings.ToLower(vLog.Address.Hex()),
	}, nil
}

// GetLatestBlockNumber 获取链上最新区块高度
func (e *Engine) GetLatestBlockNumber(ctx context.Context) (*big.Int, error) {
	header, err := e.client.HeaderByNumber(ctx, nil)
	if err != nil {
		return nil, err
	}
	return header.Number, nil
}

// Run 持续同步的守护模式
func (e *Engine) Run(ctx context.Context, startBlock *big.Int, pollInterval time.Duration) error {
	currentBlock := startBlock

	for {
		latestBlock, err := e.GetLatestBlockNumber(ctx)
		if err != nil {
			log.Printf("Error getting latest block: %v", err)
			time.Sleep(pollInterval)
			continue
		}

		// 保留6个区块确认
		safeBlock := new(big.Int).Sub(latestBlock, big.NewInt(6))
		if safeBlock.Cmp(currentBlock) < 0 {
			log.Printf("Waiting for new blocks... Current: %s, Latest: %s", currentBlock.String(), latestBlock.String())
			time.Sleep(pollInterval)
			continue
		}

		if err := e.Sync(ctx, currentBlock, safeBlock); err != nil {
			log.Printf("Sync error: %v", err)
			time.Sleep(pollInterval)
			continue
		}

		currentBlock = new(big.Int).Add(safeBlock, big.NewInt(1))
		time.Sleep(pollInterval)
	}
}
