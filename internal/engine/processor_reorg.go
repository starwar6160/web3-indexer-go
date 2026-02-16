package engine

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"math/big"
	"strings"
	"web3-indexer-go/internal/models"
)

// FindCommonAncestor 递归查找共同祖先（处理深度重组）
// 返回共同祖先的区块号和哈希，以及需要删除的区块列表
func (p *Processor) FindCommonAncestor(ctx context.Context, blockNum *big.Int) (*big.Int, string, []*big.Int, error) {
	Logger.Info("finding_common_ancestor", slog.String("from_block", blockNum.String()))

	toDelete := []*big.Int{}
	currentNum := new(big.Int).Set(blockNum)
	maxLookback := big.NewInt(1000) // 最大回退1000个块防止无限循环

	for currentNum.Cmp(big.NewInt(0)) > 0 && new(big.Int).Sub(blockNum, currentNum).Cmp(maxLookback) <= 0 {
		// 从RPC获取链上区块
		rpcBlock, err := p.client.BlockByNumber(ctx, currentNum)
		if err != nil {
			return nil, "", nil, fmt.Errorf("failed to get block %s from RPC: %w", currentNum.String(), err)
		}

		// 查询本地数据库中相同高度的区块
		var localBlock models.Block
		err = p.db.GetContext(ctx, &localBlock,
			"SELECT hash FROM blocks WHERE number = $1", currentNum.String())

		if err == sql.ErrNoRows {
			// 本地没有这个区块，继续往前找
			toDelete = append(toDelete, new(big.Int).Set(currentNum))
			currentNum.Sub(currentNum, big.NewInt(1))
			continue
		}
		if err != nil {
			return nil, "", nil, fmt.Errorf("database error at block %s: %w", currentNum.String(), err)
		}

		// 检查哈希是否匹配
		if strings.EqualFold(localBlock.Hash, rpcBlock.Hash().Hex()) {
			// 找到共同祖先！
			Logger.Info("common_ancestor_found",
				slog.String("block", currentNum.String()),
				slog.String("hash", localBlock.Hash),
			)
			return currentNum, localBlock.Hash, toDelete, nil
		}

		// 哈希不匹配，这个区块也在重组链上，需要删除
		toDelete = append(toDelete, new(big.Int).Set(currentNum))

		// 继续查找父区块（使用RPC返回的parent hash）
		parentNum := new(big.Int).Sub(currentNum, big.NewInt(1))
		currentNum.Set(parentNum)
	}

	return nil, "", nil, fmt.Errorf("common ancestor not found within %s blocks", maxLookback.String())
}

// HandleDeepReorg 处理深度重组（超过1个块的重组）
// 调用此函数前必须停止Fetcher并清空其队列
func (p *Processor) HandleDeepReorg(ctx context.Context, blockNum *big.Int) (*big.Int, error) {
	// 查找共同祖先
	ancestorNum, _, toDelete, err := p.FindCommonAncestor(ctx, blockNum)
	if err != nil {
		return nil, fmt.Errorf("failed to find common ancestor: %w", err)
	}

	LogReorgHandled(len(toDelete), ancestorNum.String())

	// 在单个事务内执行回滚（保证原子性）
	dbTx, err := p.db.BeginTxx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return nil, fmt.Errorf("failed to begin reorg transaction: %w", err)
	}
	defer func() {
		if err := dbTx.Rollback(); err != nil && err != sql.ErrTxDone {
			Logger.Warn("reorg_rollback_failed", "err", err)
		}
	}()

	// 批量删除所有分叉区块（cascade 会自动删除 transfers）
	if len(toDelete) > 0 {
		// 找到最小的要删除的块号
		minDelete := toDelete[0]
		for _, num := range toDelete {
			if num.Cmp(minDelete) < 0 {
				minDelete = num
			}
		}
		// 删除所有 >= minDelete 的块（更高效）
		_, err := dbTx.ExecContext(ctx, "DELETE FROM blocks WHERE number >= $1", minDelete.String())
		if err != nil {
			return nil, fmt.Errorf("failed to delete reorg blocks: %w", err)
		}
	}

	// 更新 checkpoint 回退到祖先高度
	_, err = dbTx.ExecContext(ctx, `
		INSERT INTO sync_checkpoints (chain_id, last_synced_block)
		VALUES ($1, $2)
		ON CONFLICT (chain_id) DO UPDATE SET
			last_synced_block = EXCLUDED.last_synced_block,
			updated_at = NOW()
	`, 1, ancestorNum.String())
	if err != nil {
		return nil, fmt.Errorf("failed to update checkpoint during reorg: %w", err)
	}

	// 提交事务
	if err := dbTx.Commit(); err != nil {
		return nil, fmt.Errorf("failed to commit reorg transaction: %w", err)
	}

	Logger.Info("deep_reorg_handled",
		slog.String("resume_block", new(big.Int).Add(ancestorNum, big.NewInt(1)).String()),
	)

	return ancestorNum, nil
}
