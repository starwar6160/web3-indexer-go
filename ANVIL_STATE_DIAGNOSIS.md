# Anvil 状态诊断报告

**日期**: 2026-02-20
**问题**: `make test-a2` 启动后 Latest (on Chain) 显示 32000+
**诊断**: ✅ 正常行为，不是状态残留

---

## 📊 实际状态

### Anvil 链状态
```bash
$ curl -s http://127.0.0.1:8545 ...
Current Block: 35232
```

### 索引器状态
```json
{
  "latest_block": "35201",      // Latest (on Chain)
  "latest_indexed": "35196",     // Total (Synced)
  "memory_sync": "35201",       // Memory Sync
  "sync_lag": 5,                // 正常滞后
  "state": "running"            // 正常运行
}
```

### 启动日志
```json
{
  "msg": "✅ REALITY_CHECK: State aligned with RPC",
  "rpc_height": 33497,          // Anvil 当时的实际高度
  "mem_height": 0               // 索引器启动状态（清空）
}
```

---

## ✅ 诊断结论

### 1. 启动时现实检查正常工作
- ✅ 索引器启动时内存高度 = 0（已清空）
- ✅ RPC 高度 = 33497（Anvil 真实高度）
- ✅ 状态对齐，没有悖论

### 2. "32000 多"是正常的 Anvil 高度
- ✅ Anvil 是持久化的，重启后高度不会清零
- ✅ 从昨天（2月19日）运行到现在，已经挖到 35232 块
- ✅ 索引器正在实时同步，滞后仅 5 块

### 3. 现实坍缩机制正常工作
- ✅ 启动时执行 `REALITY_CHECK`
- ✅ 运行时每 30 秒执行 `auditReality`
- ✅ 没有检测到"未来人"状态

---

## 🤔 可能的误解

### 误解 1: "32000 多是状态残留"
❌ **错误理解**: 索引器保留了旧的高度
✅ **实际情况**: Anvil 本身就在 35232 高度，索引器正在实时同步

### 误解 2: "应该从 0 开始"
❌ **错误理解**: Anvil 重启后高度应该归零
✅ **实际情况**: Anvil 默认持久化，除非手动清空数据

### 误解 3: "make test-a2 应该清空 Anvil"
❌ **错误理解**: `test-a2` 会重置 Anvil 链
✅ **实际情况**: `test-a2` 只启动索引器，不重置 Anvil

---

## 🛠️ 如果需要完全重置

### 方案 1: 重启 Anvil（清空链状态）
```bash
# 停止并删除 Anvil 容器
docker stop web3-demo2-anvil
docker rm web3-demo2-anvil

# 重新启动 Anvil（会从 0 开始）
# (通过 docker-compose 或 make infra-up)
```

### 方案 2: 使用 --reset 标志
```bash
# 在 test-a2 中添加 --reset 标志
# 修改 makefiles/docker.mk 第 60 行：
go run cmd/indexer/*.go --reset
```

### 方案 3: 使用 Anvil 的临时模式
```bash
# 修改 docker-compose.yml，添加 --anvil.temp
anvil:
  command: ["anvil", "--host", "0.0.0.0", "--anvil.temp"]
```

---

## 📈 当前系统状态

### 健康检查
- ✅ Anvil 高度：35232（正常运行）
- ✅ 索引器高度：35201（滞后 5 块，正常）
- ✅ 同步延迟：< 1 秒
- ✅ BPS：> 0（正在同步）
- ✅ 系统状态：running

### 现实坍缩机制
- ✅ 启动时检查：已执行，无悖论
- ✅ 运行时审计：30 秒周期，正常
- ✅ UI 显示：正常，无 `DETACHED` 状态
- ✅ AI 诊断：`parity_check: healthy`

---

## 🎯 建议

### 如果只是测试功能
- ✅ **无需任何操作**
- ✅ 当前状态完全正常
- ✅ 索引器正在实时同步 Anvil 链

### 如果需要从创世块测试
1. 重启 Anvil（方案 1）
2. 使用 `--reset` 标志（方案 2）
3. 使用 Anvil 临时模式（方案 3）

### 如果需要验证现实坍缩机制
1. 让 Anvil 运行到 10000 块
2. 停止索引器
3. 重启 Anvil（会回落到 0）
4. 启动索引器
5. 观察日志输出 `REALITY_PARADOX_DETECTED`

---

## 📝 总结

**问题**: `make test-a2` 启动后显示 32000+
**原因**: Anvil 本身就在这个高度，不是状态残留
**状态**: ✅ 完全正常
**建议**: 无需操作，除非需要完全重置

---

**最后更新**: 2026-02-20
**系统状态**: ✅ 健康
**索引器状态**: ✅ 正常运行
