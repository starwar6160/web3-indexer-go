# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Fixed
- TailFollow 边界保护：防止调度超出链顶的块
- Range Teleport 精确化：防止正常空块误触发进度跳跃
- ChainID 感知起始块：Anvil 环境现在正确从块 0 开始

### Changed
- WebSocket 事件字段格式统一对齐

## [2026-02-19]

### Added
- ChainID 感知的默认起始块逻辑 (`getDefaultStartBlockForChain()`)
- TailFollow 边界保护（aggressiveTarget 和 nextBlock 检查）
- nil 安全的 blockLabel 变量

### Fixed
- **tailfollow**: 防止追赶模式下调度超出 tip
  - 添加 aggressiveTarget 边界检查
  - 添加 nextBlock 边界检查
  - 修复 lastScheduled 更新逻辑
  
- **sequencer**: Range Teleport 触发条件精确化
  - 从 `data.Block == nil` 改为 `blockNum == nil && data.Block == nil`
  - 防止正常空块被错误跳过

- **startup**: Anvil 环境起始块修复
  - Anvil (31337): 0 (之前错误使用 10262444)
  - Sepolia (11155111): 10262444
  - Mainnet (1): 0

### Technical Details
- 提交数: 4 个原子提交
- 修改文件: 2 个
- 代码行数: +63, -18
