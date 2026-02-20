# Anvil 磁盘空间修复总结

## 问题描述（2026-02-18）

### 初始状态
- **磁盘占用**: web3-demo2-anvil 容器占用 239GB 磁盘空间
- **根分区使用率**: 84%（371GB/466GB，仅剩 72GB）
- **风险**: 100% 磁盘爆满风险，SSD 寿命损耗

### 根本原因
1. **Anvil 内部状态缓存机制**: 每小时生成快照，尽管单个只有 ~500KB
2. **缺少存储限制**: docker-compose.yml 中没有配置 tmpfs 或磁盘限制
3. **Docker overlay2 写层持续增长**: 容器运行时间越长，占用空间越大

### 解决方案

#### 1. 预防性配置（docker-compose.yml）
```yaml
anvil:
  command: [
    "--host", "0.0.0.0",
    "--port", "8545",
    "--block-time", "1"
  ]
  deploy:
    resources:
      limits:
        memory: 2G
  tmpfs:
    - /home/foundry/.foundry/anvil/tmp:rw,size=100M,noexec,nosuid,nodev
    - /tmp:rw,size=50M,noexec,nosuid,nodev
  healthcheck:
    test: ["CMD", "curl", "-f", "http://localhost:8545"]
    interval: 30s
    timeout: 10s
    retries: 3
    start_period: 5s
```

**关键改动**:
- Memory Limit: 2GB（防止内存泄漏）
- tmpfs: 100MB（挂载快照目录到内存）
- tmpfs: 50MB（通用临时文件目录）
- Healthcheck: 确保 Anvil 响应正常

#### 2. 自动化监控

**磁盘监控脚本** (`scripts/infra/disk-monitor.sh`):
- 80% 警告阈值
- 90% 严重告警阈值
- 检查 Anvil tmpfs 使用情况
- 生成结构化日志

**紧急清理脚本** (`scripts/infra/anvil-emergency-cleanup.sh`):
- 安全停止并删除 Anvil 容器
- 清理 Docker 悬空资源
- 自动重启 Anvil
- 详细的日志记录

#### 3. Makefile 集成

新增命令:
- `make check-disk-space`: 检查磁盘空间
- `make anvil-emergency-cleanup`: 紧急清理
- `make anvil-disk-usage`: 查看 Anvil 磁盘使用详情

#### 4. 维护脚本增强

在 `scripts/infra/anvil-maintenance.sh` 中添加:
- tmpfs 使用量检查
- 80% 使用率告警
- 与新的监控脚本集成

## 验证结果

### 磁盘空间
- **修复前**: 84% 使用率（371GB/466GB）
- **修复后**: 30% 使用率（132GB/466GB）
- **释放空间**: ~239GB

### tmpfs 配置
```
Filesystem      Size  Used Avail Use% Mounted on
tmpfs           100M     0  100M   0% /home/foundry/.foundry/anvil/tmp
tmpfs            50M     0   50M   0% /tmp
```

### 内存限制
```
Memory Limit: 2 GB
```

### Anvil 功能
- ✅ RPC 正常响应（区块 #86）
- ✅ Healthcheck 配置正确
- ✅ 索引器可以正常连接

## 原子提交

本次修复包含 6 个原子提交：

1. **feat(docker): add Anvil storage limits**
   - 添加 tmpfs 和内存限制
   - 添加 healthcheck 配置

2. **feat(monitor): create disk space monitoring script**
   - 创建 `disk-monitor.sh`
   - 80%/90% 告警阈值

3. **feat(cleanup): create Anvil emergency cleanup script**
   - 创建 `anvil-emergency-cleanup.sh`
   - 自动化清理流程

4. **feat(maintenance): enhance Anvil maintenance script**
   - 添加 tmpfs 监控
   - 添加 80% 告警

5. **feat(makefile): add disk management targets**
   - 添加 `make check-disk-space`
   - 添加 `make anvil-emergency-cleanup`
   - 添加 `make anvil-disk-usage`

6. **docs(memory): document Anvil disk management**
   - 更新 MEMORY.md
   - 记录问题和解决方案

## 后续建议

### 立即执行
1. 配置系统 crontab 定时任务:
   ```bash
   # 每 30 分钟检查磁盘空间
   */30 * * * * /home/ubuntu/zwCode/web3-indexer-go/scripts/infra/disk-monitor.sh

   # 每天凌晨 3 点清理 Docker 资源
   0 3 * * * /usr/bin/docker system prune -f --volumes > /dev/null 2>&1
   ```

### 长期监控
1. 每周运行 `make check-disk-space`
2. 关注 tmpfs 使用趋势（应该保持在 < 50MB）
3. 如果磁盘使用率再次超过 80%，运行 `make anvil-emergency-cleanup`

### 最佳实践
1. **定期监控**: 每周检查一次磁盘空间
2. **自动清理**: 保持 cron 任务运行
3. **tmpfs 限制**: 防止 Anvil 写入持久化存储
4. **内存限制**: 防止容器内存泄漏影响系统

## 文件清单

### 修改的文件
1. `configs/docker/docker-compose.yml` - Anvil 服务配置
2. `makefiles/docker.mk` - 新增磁盘管理命令
3. `scripts/infra/anvil-maintenance.sh` - 增强 tmpfs 监控
4. `MEMORY.md` - 添加磁盘管理章节

### 新增的文件
1. `scripts/infra/disk-monitor.sh` - 磁盘监控脚本
2. `scripts/infra/anvil-emergency-cleanup.sh` - 紧急清理脚本
3. `scripts/verify-disk-fix.sh` - 验证脚本
4. `ANVIL_DISK_FIX_SUMMARY.md` - 本文档

## 测试验证

所有验证已通过：
- ✅ 磁盘空间: 30% (从 84% 降至 30%)
- ✅ tmpfs 配置: 100M 限制生效
- ✅ 内存限制: 2GB 限制生效
- ✅ RPC 响应: 正常
- ✅ Healthcheck: 配置正确
- ✅ 监控脚本: 工作正常
- ✅ 清理脚本: 就绪待命

## 风险评估

| 阶段 | 风险等级 | 影响 | 缓解措施 |
|------|---------|------|----------|
| 阶段 1（清理） | LOW | Anvil 停机 2-3 分钟 | Testnet 不受影响 |
| 阶段 2（配置） | LOW | 配置错误可能启动失败 | 已验证配置正确 |
| 阶段 3（自动化） | LOW | Cron 任务失败 | 日志监控 |
| 阶段 4（验证） | LOW | 检测延迟 | 手动验证完成 |

---

**修复日期**: 2026-02-18
**维护者**: 追求 6 个 9 持久性的资深后端工程师
**状态**: ✅ 完成并验证
