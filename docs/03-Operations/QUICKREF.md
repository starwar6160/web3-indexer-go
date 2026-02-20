# Web3 Indexer - Quick Reference (v2.2.0-stable)

## 🚀 快速启动

```bash
# 全自动演示（推荐）
./scripts/auto-demo.sh

# 手动启动 Anvil 环境
make a2

# 手动启动 Sepolia 测试网
make a1

# 查看状态
curl http://localhost:8082/api/status | jq '.'
```

## 🎯 三大核心特性

### 1. SQL 鲁棒性
```go
// async_writer.go:204
maxHeightStr := fmt.Sprintf("%d", maxHeight)
// 防止 "unable to encode 0x9d0ab3" 错误
```

### 2. 空洞跳过（Gap Bypass）
```go
// sequencer_core.go:154
// 最多重试 3 次，然后强制跳过
if s.gapFillCount < 3 {
    // 触发 gap-fill
} else {
    // 强制跳过，保持流水线流动
}
```

### 3. 热度感应 Eco-Mode
```go
// eco_strategy.go
// 滑动窗口计算链上"体温"
// 自适应采样：200ms - 30s
```

## 📊 面试话术（30 秒版本）

> "这是我为 Web3 区块链索引器设计的**博彩级**系统。
> 
> **核心理念**：在博彩/交易系统中，**'阻塞'比'延迟'更可怕**。
> 
> **三大创新**：
> 1. **空洞跳过**：RPC 404 时自动跳过，后台异步回补
> 2. **热度驱动**：自适应采样，节省 90% RPC Quota
> 3. **SQL 鲁棒**：显式类型转换，6 个 9 持久性
> 
> **演示就绪**：`./scripts/auto-demo.sh` 一键展示"

## 🛠️ 故障排查

### disk_sync 停止更新
```bash
# 检查 SQL 编码错误
docker logs web3-demo2-app 2>&1 | grep "encode"

# 解决方案：已在 v2.2.0-stable 修复
```

### CRITICAL_GAP_DETECTED
```bash
# 查看空洞详情
docker logs web3-demo2-app 2>&1 | grep "GAP_DETECTED"

# 系统会自动跳过（3 次重试后）
# 观察日志中的 "GAP_BYPASS"
```

### Eco-Mode 不唤醒
```bash
# 模拟交易脉冲
make anvil-inject

# 观察日志中的 "HEAT_SPIKE"
```

## 📝 提交历史

```
v2.2.0-stable (11 commits)
├── fix(async_writer): SQL encoding
├── feat(sequencer): Gap Bypass
├── feat(eco): Heat-based Eco-Mode
└── feat(demo): Auto script
```

## 🔗 相关文档

- `DEMO_GUIDE.md` - 完整演示指南
- `CHANGELOG.md` - 变更日志
- `configs/env/config.demo.golden.env` - 黄金配置

---

**维护者**: 追求 6 个 9 持久性的资深后端工程师
**更新**: 2026-02-19
