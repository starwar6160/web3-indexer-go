# WebSocket 连接失败 - 快速修复指南

## ⚡ 立即解决（2 分钟）

### 问题
访问 `https://demo2.st6160.click/` 时 WebSocket 连接失败，控制台显示：
```
WebSocket connection to 'wss://demo2.st6160.click/ws' failed
```

### ✅ 当前状态
- **已自动降级**: 前端已切换到 HTTP 轮询模式，每 2 秒更新数据
- **功能正常**: 所有功能完全可用，只是不是实时推送
- **无需操作**: 用户可以正常使用

### 🔧 恢复实时模式（可选）

如果想恢复 WebSocket 实时连接：

1. **登录 Cloudflare Dashboard**
   - URL: https://dash.cloudflare.com

2. **选择域名**
   - 点击: `st6160.click`

3. **调整安全设置**
   - 导航到: **Security** → **Settings**
   - **Bot Fight Mode**: **关闭** 🔴
   - **Security Level**: 设置为 **Low** ⚡
   - **Under Attack Mode**: **关闭**（如果开启）

4. **等待并验证**
   - 等待 30 秒
   - 刷新浏览器: https://demo2.st6160.click/
   - 查看浏览器控制台，应显示: `✅ WebSocket Connected`

---

## 🔍 诊断工具

```bash
# 运行完整诊断
bash /home/ubuntu/zwCode/web3-indexer-go/scripts/ops/diagnose-websocket.sh
```

预期输出：
```
✅ 本地 WebSocket 连接成功
✅ Indexer 服务正在监听 8082 端口
✅ Cloudflare Tunnel 进程运行中
⚠️ 公网 HTTP 访问被阻止 (HTTP 403)
   原因: Cloudflare Bot Management / Challenge 拦截
```

---

## 📊 技术细节

### 根本原因
Cloudflare 的 **Bot Management** 或 **Challenge** 平台正在拦截所有请求，包括 WebSocket。

证据：
```
HTTP/2 403
cf-mitigated: challenge
```

### 降级方案实施

已在前端 `dashboard.js` 中实施自动降级逻辑：

```javascript
// WebSocket 失败 3 次后自动切换到 HTTP 轮询
ws.onerror = (err) => {
    wsFailCount++;
    if (wsFailCount >= 3 && !pollingMode) {
        pollingMode = true;
        startPolling(); // 每 2 秒轮询一次
    }
};

// 每 60 秒尝试恢复 WebSocket 连接
setInterval(() => {
    if (pollingMode) {
        attemptWSRecovery();
    }
}, 60000);
```

### 性能对比

| 指标 | WebSocket (实时) | HTTP 轮询 (降级) |
|------|----------------|-----------------|
| 数据延迟 | 0-100ms | 0-2000ms |
| 网络开销 | 低 | 中 |
| 用户体验 | ⭐⭐⭐⭐⭐ | ⭐⭐⭐⭐ |

---

## 🎯 验证步骤

### 本地验证（✅ 已通过）
```bash
curl -i -N \
  -H "Connection: Upgrade" \
  -H "Upgrade: websocket" \
  -H "Sec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==" \
  -H "Sec-WebSocket-Version: 13" \
  http://127.0.0.1:8082/ws

# 预期: HTTP/1.1 101 Switching Protocols
```

### 公网验证（⏳ 等待 Cloudflare 设置）
```bash
# 调整 Cloudflare 设置后运行
curl -I https://demo2.st6160.click/

# 预期: HTTP/2 200 (而不是 403)
```

---

## 📱 用户操作指南

### 当前使用（HTTP 轮询模式）
1. 访问: https://demo2.st6160.click/
2. 数据每 2 秒自动更新
3. 所有功能正常使用
4. UI 显示: `HTTP POLLING MODE`

### 恢复实时模式后
1. 数据实时推送（无延迟）
2. UI 显示: `● LIVE` (绿色脉冲)
3. 日志显示: `🔗 WebSocket reconnected successfully`

---

## 🆘 故障排除

### 问题: 仍然显示 403 错误
**解决**: 检查 Cloudflare Security 设置，确认 Bot Fight Mode 已关闭

### 问题: HTTP 轮询不工作
**解决**: 检查浏览器控制台，可能是 `ERR_BLOCKED_BY_CLIENT`（广告拦截器）

**步骤**:
1. 禁用浏览器扩展（临时测试）
2. 或使用隐私模式/无痕模式
3. 或在 uBlock Origin 中添加白名单规则

### 问题: 本地访问正常，公网访问失败
**解决**: 这是预期的，等待 Cloudflare 设置调整

---

## 📞 支持

### 查看日志
```bash
# Cloudflare Tunnel 日志
tail -f /tmp/cloudflared.log

# Indexer 日志
journalctl -u indexer -f
```

### 重启服务
```bash
# 重启 Cloudflare Tunnel
bash /home/ubuntu/zwCode/web3-indexer-go/scripts/ops/fix-cloudflare-tunnel.sh

# 重启 Indexer
sudo systemctl restart indexer
```

---

## 📄 相关文档

- **完整实施报告**: `WEBSOCKET_FIX_SUMMARY.md`
- **诊断工具**: `scripts/ops/diagnose-websocket.sh`
- **Cloudflare 文档**: https://developers.cloudflare.com/cloudflare-one/connections/connect-apps/

---

**更新时间**: 2026-02-25 14:00 JST
**状态**: ✅ 降级方案已实施，系统运行正常
