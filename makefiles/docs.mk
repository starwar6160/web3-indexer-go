# --- 文档自动化 (Documentation) ---

.PHONY: docs-sync

docs-sync:
	@echo "📚 Synchronizing documentation..."
	@mkdir -p docs/01-Architecture docs/02-Logic docs/03-Operations
	@# 自动移动散落在根目录或旧目录的文件到新结构
	@mv docs/01-Architecture/LazyIndexMode.md docs/02-Logic/ 2>/dev/null || true
	@mv docs/99-Operations/* docs/03-Operations/ 2>/dev/null || true
	@go run scripts/generate_docs_index.go
	@echo "✅ Documentation index updated in docs/SUMMARY.md"
