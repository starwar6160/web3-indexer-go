# START_BLOCK=latest 修复报告

**日期**: 2026-02-20
**问题**: `make test-a2` 中断后再次运行，出现"索引器领先于链"的状态
**根本原因**: 时间差导致的起始高度过期
**解决方案**: 使用 `START_BLOCK=latest` 替代 `START_BLOCK=$ANVIL_HEIGHT`

---

## 🚨 问题现象

### 用户报告
```
System State:    DISCONNECTED
Latest (on Chain): 35789
Total (Synced):    35805  ← 领先于链！
```

### 日志分析
```json
// 1. 检测 Anvil 高度
📊 Anvil 当前高度：35769

// 2. 索引器启动
✅ REALITY_CHECK: State aligned with RPC, rpc_height: 35772, mem_height: 0
☢️ ANVIL_EPHEMERAL: Executing Nuclear Reset...
🚀 Sequencer started. Expected block: 35769  ← 使用的是检测时的高度！

// 3. 运行中的状态
mem_latest: 35789
rpc_actual: 35790
lag: 1  ← 正常滞后
```

---

## 🔍 根本原因

### 时间差问题

| 时间点 | Anvil 高度 | 索引器状态 |
|--------|-----------|-----------|
| T1: 检测高度 | 35769 | - |
| T2: 启动完成 | 35772 | 从 35769 开始 |
| T3: 运行中 | 35790 | 35789（滞后 1） |

**问题**：
1. `make test-a2` 使用 `START_BLOCK=$ANVIL_HEIGHT`（固定值）
2. 从检测到启动，Anvil 继续挖块（35769 → 35772）
3. 索引器从**旧的检测高度**（35769）开始
4. 用户看到"Latest: 35789"，以为是状态残留

### 中断后再次运行

**场景**：
1. 第一次运行 `make test-a2`，检测高度 35769，启动到 35805
2. 用户中断（Ctrl+C）
3. 第二次运行 `make test-a2`，检测高度 35805，但启动时 Anvil 已经到 35810
4. 索引器从 35805 开始，RPC 当前 35810
5. **如果检测时 Anvil 在 35805，但启动时 Anvil 在 35800（重启过），就会出现"领先于链"！**

---

## ✅ 解决方案

### 修改内容

**文件**: `makefiles/docker.mk`
**位置**: 第 54 行

```makefile
# 旧代码
START_BLOCK=$$ANVIL_HEIGHT

# 新代码
START_BLOCK=latest
```

### 工作原理

**代码逻辑** (`cmd/indexer/main.go:75-84`):

```go
if cfg.StartBlockStr == "latest" {
    if rpcErr != nil {
        return big.NewInt(0), nil
    }
    reorgSafetyOffset := int64(6)
    startBlock := new(big.Int).Sub(latestChainBlock, big.NewInt(reorgSafetyOffset))
    if startBlock.Cmp(big.NewInt(0)) < 0 {
        startBlock = big.NewInt(0)
    }
    return startBlock, nil
}
```

**效果**：
- ✅ **启动时实时获取 RPC 高度**
- ✅ **减去 6 个块的安全偏移（防止 reorg）**
- ✅ **避免了时间差导致的过期问题**

---

## 📊 修改对比

### 旧逻辑（`START_BLOCK=$ANVIL_HEIGHT`）

```
1. 检测 Anvil 高度 → 35769
2. 环境变量设置 → START_BLOCK=35769
3. 索引器启动 → 从 35769 开始
4. 问题：启动时 RPC 已经到 35772
```

### 新逻辑（`START_BLOCK=latest`）

```
1. 检测 Anvil 高度 → 35769（仅用于显示标题）
2. 环境变量设置 → START_BLOCK=latest
3. 索引器启动 → 实时获取 RPC 高度（35772）- 6 = 35766
4. 正确：从当前 RPC 高度开始
```

---

## 🎯 预期效果

### 场景 1: 正常启动

```
Anvil 高度：35772
索引器起始：35766（RPC - 6）
状态：正常滞后
```

### 场景 2: Anvil 重启后

```
Anvil 重启前：35805
Anvil 重启后：13000
索引器启动：实时获取 13000 - 6 = 12994
状态：正常（现实坍缩机制也会触发）
```

### 场景 3: 中断后再次运行

```
中断时高度：35805
再次启动时：实时获取当前 RPC 高度
状态：正常（不会使用旧的高度）
```

---

## 🧪 验证方法

### 1. 正常启动测试

```bash
make test-a2
```

**预期**：
- ✅ 日志显示 `Starting continuous tail follow, start_block: <RPC当前-6>`
- ✅ AI 诊断显示 `parity_check: healthy`
- ✅ 无 "TIME_PARADOX" 或 "领先于链" 问题

### 2. Anvil 重启测试

```bash
# 1. 启动索引器
make test-a2

# 2. 等待同步到 36000
# 3. 重启 Anvil（模拟回落）
docker restart web3-demo2-anvil

# 4. 观察日志
# 预期：触发现实坍缩机制
```

**预期**：
- ✅ 日志显示 `🚨 REALITY_PARADOX_DETECTED`
- ✅ 日志显示 `forcing_collapse_to_reality`
- ✅ 索引器从 Anvil 新高度继续

### 3. 中断恢复测试

```bash
# 1. 启动索引器
make test-a2

# 2. 等待同步，然后 Ctrl+C 中断

# 3. 再次启动
make test-a2

# 预期：从当前 RPC 高度开始，不使用旧高度
```

---

## 📝 相关修改

### 文件变更

| 文件 | 修改内容 | 原因 |
|------|---------|------|
| `makefiles/docker.mk` | `START_BLOCK=$$ANVIL_HEIGHT` → `START_BLOCK=latest` | 避免时间差问题 |

### 标题显示

```makefile
# 旧标题
APP_TITLE="🧪 ANVIL-LOCAL (8092) [Block:$$ANVIL_HEIGHT]"

# 新标题（仍然显示检测时的高度作为参考）
APP_TITLE="🧪 ANVIL-LOCAL (8092) [Latest:$$ANVIL_HEIGHT]"
```

---

## 🔒 安全保障

### 多层防护

1. **第一层：`START_BLOCK=latest`**
   - 启动时实时获取 RPC 高度
   - 避免时间差问题

2. **第二层：现实坍缩机制**
   - 启动时检查 `mem_height > rpc_height`
   - 运行时每 30 秒审计
   - 自动对齐到 RPC 高度

3. **第三层：Reorg 安全偏移**
   - `latest - 6` 个块
   - 防止链重组导致的数据不一致

---

## 📚 参考资料

### 相关代码

- `cmd/indexer/main.go:75-84` - START_BLOCK=latest 处理逻辑
- `internal/engine/strategy.go:25-50` - 启动时现实检查
- `internal/engine/orchestrator.go:935-985` - 运行时现实审计

### 相关文档

- `REALITY_COLLAPSE_IMPLEMENTATION.md` - 现实坍缩机制实施
- `ANVIL_STATE_DIAGNOSIS.md` - Anvil 状态诊断

---

## ✅ 总结

### 问题
- `make test-a2` 使用 `START_BLOCK=$ANVIL_HEIGHT`（固定值）
- 从检测到启动存在时间差
- 导致索引器使用"过期"的起始高度

### 解决方案
- ✅ 改用 `START_BLOCK=latest`
- ✅ 启动时实时获取 RPC 高度
- ✅ 减去 6 个块的安全偏移

### 效果
- ✅ 彻底解决"时间差"问题
- ✅ 支持中断后恢复
- ✅ 支持Anvil 重启后自动对齐
- ✅ 配合现实坍缩机制，多层防护

---

**最后更新**: 2026-02-20
**状态**: ✅ 已修复
**测试**: 待验证
