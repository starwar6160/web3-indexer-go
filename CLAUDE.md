# 项目治理规则 (Senior Backend Context)

## 核心约束 (Token Saving)
- **禁止全量扫描**：严禁在未获准的情况下运行 `find .` 或 `grep -r`。仅限读取当前任务直接相关的文件。
- **目录剪枝**：忽略所有 `node_modules`, `.git`, `dist`, `build`, `out`, `vendor`, `tmp` 文件夹。
- **缓存友好**：保持回复简洁，不要重复描述已知的项目结构或输出冗长的总结，除非显式要求。

## 技术栈与规范
- **后端**: Go (Standard Layout), C++ 20 (Smart Pointers, RAII).
- **数据库**: Prisma (PostgreSQL), Redis.
- **密码学规范**: 必须使用 `secure_memzero` 处理敏感内存。禁止在 C++ 中使用原始指针管理生命周期。
- **Go 并发**: 必须检查 `context.Done()`，严禁 goroutine 泄露。

## 常用指令 (Verified Commands)
- **开发**: `yarn dev` | `go run main.go`
- **构建**: `yarn build` | `make build`
- **数据库**: `npx prisma generate` | `npx prisma migrate dev`
- **测试**: `go test ./...` | `make test`
- **清理**: `rm -rf dist build out`
- **质量检查**: `make qa` 

## 协作模式
- **Plan First**: 任何修改前，必须在 `plan` 模式下列出影响的文件路径。
- **增量读取**: 仅读取计划中列出的文件。如果需要查看定义，优先搜索特定文件而非模糊搜索。
- **Refactor Rule**: 优先保持函数单一职责，重构时不要触动未修改逻辑。
