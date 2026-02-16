# ==============================================================================
# 数据库管理命令 (Database Management)
# ==============================================================================

# 数据库连接配置
DB_USER := postgres
DB_CONTAINER := web3-testnet-db
DB_HOST := web3-testnet-db
DEMO1_DB := web3_indexer_demo1
DEMO2_DB_CONTAINER := web3-demo2-db
DEMO2_DB := web3_indexer_demo2
DEBUG_DB := web3_indexer_debug

.PHONY: db-list db-clean-debug db-reset-debug db-sync-schema db-backup-demo1 db-clean-demo2 db-reset-demo2

## 📊 查看所有 Web3 数据库
db-list:
	@echo "📊 Web3 Indexer 数据库列表："
	@docker exec $(DB_CONTAINER) psql -U $(DB_USER) -d postgres -c "SELECT datname, pg_size_pretty(pg_database_size(datname)) as size FROM pg_database WHERE datname LIKE 'web3%' ORDER BY datname;"
	@echo ""
	@echo "📈 各数据库数据统计："
	@echo "Demo1 (8081):"
	@docker exec $(DB_CONTAINER) psql -U $(DB_USER) -d $(DEMO1_DB) -c "SELECT COUNT(*) as blocks FROM blocks;" 2>/dev/null || echo "  无数据"
	@docker exec $(DB_CONTAINER) psql -U $(DB_USER) -d $(DEMO1_DB) -c "SELECT COUNT(*) as transfers FROM transfers;" 2>/dev/null || echo "  无数据"
	@echo "Debug (8083):"
	@docker exec $(DB_CONTAINER) psql -U $(DB_USER) -d $(DEBUG_DB) -c "SELECT COUNT(*) as blocks FROM blocks;" 2>/dev/null || echo "  无数据"
	@docker exec $(DB_CONTAINER) psql -U $(DB_USER) -d $(DEBUG_DB) -c "SELECT COUNT(*) as transfers FROM transfers;" 2>/dev/null || echo "  无数据"
	@echo "Demo2 (8082):"
	@docker exec $(DEMO2_DB_CONTAINER) psql -U $(DB_USER) -d $(DEMO2_DB) -c "SELECT COUNT(*) as blocks FROM blocks;" 2>/dev/null || echo "  无数据"
	@docker exec $(DEMO2_DB_CONTAINER) psql -U $(DB_USER) -d $(DEMO2_DB) -c "SELECT COUNT(*) as transfers FROM transfers;" 2>/dev/null || echo "  无数据"

## 🧹 清空 Debug 数据库数据（保留表结构）
db-clean-debug:
	@echo "🧹 清空 Debug 数据库数据（保留表结构）..."
	@docker exec $(DB_CONTAINER) psql -U $(DB_USER) -d $(DEBUG_DB) -c "TRUNCATE TABLE transfers, blocks, transactions, logs, sync_checkpoints, sync_status, visitor_stats RESTART IDENTITY CASCADE;"
	@echo "✅ Debug 数据库已清空"

## 🔄 重置 Debug 数据库（删除并重建）
db-reset-debug:
	@echo "🔄 重置 Debug 数据库（删除并重建）..."
	@docker exec $(DB_CONTAINER) psql -U $(DB_USER) -d postgres -c "DROP DATABASE IF EXISTS $(DEBUG_DB);"
	@docker exec $(DB_CONTAINER) psql -U $(DB_USER) -d postgres -c "CREATE DATABASE $(DEBUG_DB);"
	@echo "📋 复制表结构从 Demo1..."
	@docker exec $(DB_CONTAINER) pg_dump -U $(DB_USER) -s $(DEMO1_DB) | docker exec -i $(DB_CONTAINER) psql -U $(DB_USER) -d $(DEBUG_DB)
	@echo "✅ Debug 数据库已重置"

## 🔄 同步 Schema（从 Demo1 同步到 Debug）
db-sync-schema:
	@echo "🔄 同步 Schema 从 $(DEMO1_DB) 到 $(DEBUG_DB)..."
	@docker exec $(DB_CONTAINER) pg_dump -U $(DB_USER) -s $(DEMO1_DB) | docker exec -i $(DB_CONTAINER) psql -U $(DB_USER) -d $(DEBUG_DB)
	@echo "✅ Schema 同步完成"

## 💾 备份 Demo1 数据
db-backup-demo1:
	@echo "💾 备份 Demo1 数据到 backups/ 目录..."
	@mkdir -p backups
	@docker exec $(DB_CONTAINER) pg_dump -U $(DB_USER) -d $(DEMO1_DB) > backups/demo1_backup_$$(date +%Y%m%d_%H%M%S).sql
	@echo "✅ Demo1 备份完成：backups/demo1_backup_$$(date +%Y%m%d_%H%M%S).sql"

## 📥 恢复 Demo1 数据（从最新备份）
db-restore-demo1:
	@echo "📥 恢复 Demo1 数据（从最新备份）..."
	@latest_backup=$$(ls -t backups/demo1_backup_*.sql 2>/dev/null | head -1); \
	if [ -z "$$latest_backup" ]; then \
		echo "❌ 未找到备份文件"; \
		exit 1; \
	fi; \
	echo "📋 恢复文件: $$latest_backup"; \
	cat "$$latest_backup" | docker exec -i $(DB_CONTAINER) psql -U $(DB_USER) -d $(DEMO1_DB)
	@echo "✅ Demo1 恢复完成"

## 🧹 清空 Demo2 数据库数据（保留表结构）
db-clean-demo2:
	@echo "🧹 清空 Demo2 数据库数据（保留表结构）..."
	@docker exec $(DEMO2_DB_CONTAINER) psql -U $(DB_USER) -d $(DEMO2_DB) -c "TRUNCATE TABLE transfers, blocks, transactions, logs, sync_checkpoints, sync_status, visitor_stats RESTART IDENTITY CASCADE;"
	@echo "✅ Demo2 数据库已清空"

## 🔄 重置 Demo2 数据库（删除并重建）
db-reset-demo2:
	@echo "🔄 重置 Demo2 数据库（删除并重建）..."
	@docker exec $(DEMO2_DB_CONTAINER) psql -U $(DB_USER) -d postgres -c "DROP DATABASE IF EXISTS $(DEMO2_DB);"
	@docker exec $(DEMO2_DB_CONTAINER) psql -U $(DB_USER) -d postgres -c "CREATE DATABASE $(DEMO2_DB);"
	@echo "📋 复制表结构..."
	@docker exec $(DEMO2_DB_CONTAINER) pg_dump -U $(DB_USER) -s web3_indexer | docker exec -i $(DEMO2_DB_CONTAINER) psql -U $(DB_USER) -d $(DEMO2_DB)
	@echo "✅ Demo2 数据库已重置"
