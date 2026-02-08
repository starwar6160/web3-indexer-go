package engine

import (
	"fmt"
	"log"
	"math/big"
	"strings"
	"web3-indexer-go/internal/models"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/jmoiron/sqlx"
)

type Processor struct {
	db *sqlx.DB
}

func NewProcessor(db *sqlx.DB) *Processor {
	return &Processor{db: db}
}

// ProcessBatch 是单线程执行的，确保数据一致性
// 接收乱序的 BlockData，必须先排序或由 Fetcher 保证有序？
// 更好的策略：Fetcher 返回乱序，Processor 内部维护一个 Buffer 进行重排序
func (p *Processor) ProcessBlock(data BlockData) error {
	if data.Err != nil {
		return fmt.Errorf("fetch error: %w", data.Err)
	}
	
	block := data.Block
	log.Printf("Processing block: %d | Hash: %s", block.NumberU64(), block.Hash().Hex())

	// 开启事务 (ACID 核心)
	tx, err := p.db.Beginx()
	if err != nil {
		return err
	}
	// 无论成功失败，确保 Rollback (Commit 后 Rollback 无效)
	defer tx.Rollback()

	// 1. Reorg 检测 (Parent Hash Check)
	// 查询数据库中上一个区块
	var lastBlock models.Block
	err = tx.Get(&lastBlock, "SELECT * FROM blocks WHERE number = $1", block.NumberU64()-1)
	
	if err == nil {
		// 如果找到了上一个区块，检查 Hash 链
		if lastBlock.Hash != block.ParentHash().Hex() {
			log.Printf("🚨 REORG DETECTED at block %d! Expected parent %s, got %s", 
				block.NumberU64(), lastBlock.Hash, block.ParentHash().Hex())
			
			// 触发回滚逻辑：找到分叉点并删除 (简化版：直接删除 >= 当前高度-depth 的数据)
			// 实际生产中应递归查找 Common Ancestor
			_, err = tx.Exec("DELETE FROM blocks WHERE number >= $1", block.NumberU64()-1)
			if err != nil {
				return fmt.Errorf("reorg rollback failed: %w", err)
			}
			// 回滚后，当前块不能直接插入，应抛出特殊错误让外层重新调度 fetch
			return fmt.Errorf("reorg_handled_please_refetch") 
		}
	}

	// 2. 写入 Block
	_, err = tx.NamedExec(`
		INSERT INTO blocks (number, hash, parent_hash, timestamp)
		VALUES (:number, :hash, :parent_hash, :timestamp)
		ON CONFLICT (number) DO NOTHING
	`, models.Block{
		Number:     models.NewBigInt(block.Number().Int64()),
		Hash:       block.Hash().Hex(),
		ParentHash: block.ParentHash().Hex(),
		Timestamp:  block.Time(),
	})
	if err != nil {
		return err
	}

	// 3. 写入 Checkpoint (Update-or-Insert)
	_, err = tx.Exec(`
		INSERT INTO sync_checkpoints (chain_id, last_synced_block)
		VALUES ($1, $2)
		ON CONFLICT (chain_id) DO UPDATE SET last_synced_block = EXCLUDED.last_synced_block
	`, 1, block.NumberU64()) // 假设 ChainID 为 1
	if err != nil {
		return err
	}

	// 4. 提交事务
	return tx.Commit()
}

// ExtractTransfer 从区块日志中提取 ERC20 Transfer 事件
func (p *Processor) ExtractTransfer(vLog types.Log) *models.Transfer {
	// 检查是否为 Transfer 事件 (topic[0])
	if len(vLog.Topics) < 3 || vLog.Topics[0] != TransferEventHash {
		return nil
	}

	from := common.BytesToAddress(vLog.Topics[1].Bytes())
	to := common.BytesToAddress(vLog.Topics[2].Bytes())
	amount := new(big.Int).SetBytes(vLog.Data)

	return &models.Transfer{
		BlockNumber:  models.BigInt{Int: new(big.Int).SetUint64(vLog.BlockNumber)},
		TxHash:       vLog.TxHash.Hex(),
		LogIndex:     uint(vLog.Index),
		From:         strings.ToLower(from.Hex()),
		To:           strings.ToLower(to.Hex()),
		Amount:       models.BigInt{Int: amount},
		TokenAddress: strings.ToLower(vLog.Address.Hex()),
	}
}
