# V1 迁移至 Golang 版本核心经验总结 (V1_LESSONS.md)

基于 V1 TypeScript 版本在 ERC20 事件解析、交易入库及 Docker 环境调试中的实战经验，以下是迁移至 Golang 版本时必须采纳的 5 条通用策略。

## 1. 容器网络与 RPC 访问策略

在 V1 解决 "Anvil 监听失败" 时，核心问题在于容器网络的隔离性与回环地址的误用。

*   **问题回顾**：V1 初期在 Docker 中使用 `localhost:8545` 访问宿主机 Anvil，导致 `ECONNREFUSED`。
*   **Golang 迁移策略**：
    *   **服务发现**：在 `docker-compose.yml` 中，Golang Indexer 必须通过**服务名**（如 `http://blockchain-node:8545`）访问 RPC，严禁使用 `localhost` 或 `127.0.0.1`。
    *   **监听地址**：Anvil/Geth 节点启动时必须绑定 `0.0.0.0` (如 `anvil --host 0.0.0.0`)，否则容器外部无法访问。
    *   **端口映射**：区分宿主机端口（如 `58545`，用于本地调试）与容器内部端口（`8545`，用于服务间通信），Golang 配置中应支持通过环境变量 `RPC_URL` 动态切换。

## 2. 地址标准化与大小写处理

V1 在处理 ERC20 `Transfer` 事件时，曾因地址大小写不一致导致查询不到数据或重复入库。

*   **问题回顾**：EVM 地址是十六进制字符串，存在 Checksum 格式（如 `0xAbC...`）和纯小写格式。V1 最终在 Zod Schema 中强制转换。
*   **Golang 迁移策略**：
    *   **入库前强制小写**：在 Golang 结构体（Struct）处理逻辑中，接收到 RPC 返回的 `common.Address` 后，必须立即调用 `.Hex().ToLower()` 或 `strings.ToLower()`。
    *   **数据库查询标准化**：所有 SQL 查询条件中的地址参数也必须先转小写。
    *   **建议方案**：定义一个自定义类型 `NormalizedAddress`，在其 `UnmarshalJSON` 方法中自动完成小写转换，确保业务逻辑层永远只接触到小写地址。

## 3. ERC20 事件解析与原子性写入

V1 解决了 "交易不入库" 的关键在于从分离写入改为原子事务写入，并引入了严格的分页获取。

*   **问题回顾**：早期版本 Block 和 Logs 分开写入，且 `getLogs` 一次请求范围过大导致超时或 OOM。
*   **Golang 迁移策略**：
    *   **原子事务 (Tx)**：在 Golang 中使用 `sql.Tx` 或 GORM 的 `Transaction` 闭包。**必须**在一个事务中同时写入 `Block` 和其包含的 `Logs`。如果 Logs 解析失败，Block 也不应提交，避免由“空洞”造成的数据不一致。
    *   **强制分页 (Pagination)**：`FilterLogs` 请求的区块范围（`ToBlock - FromBlock`）不应超过 100-500（取决于链）。Golang 需实现一个分片循环器，自动将大范围请求拆解为多个 RPC 调用。

## 4. 数据库幂等性与约束设计

V1 最终通过复合唯一索引和 `ON CONFLICT` 解决了重复数据和重组问题。

*   **问题回顾**：重启索引器时，重复处理相同区块导致主键冲突崩溃。
*   **Golang 迁移策略**：
    *   **复合唯一键**：在 `transfers` 表中必须建立 `UNIQUE (block_number, log_index)` 索引。这比依赖 Transaction Hash 更可靠（同一笔交易可能发出多个同类事件）。
    *   **UPSERT 语义**：使用 PostgreSQL 的 `ON CONFLICT DO NOTHING` 或 `DO UPDATE`。在 Golang 中，使用 `gorm.Clauses(clause.OnConflict{DoNothing: true})` 或手写 SQL `INSERT ... ON CONFLICT DO NOTHING`，确保重复索引同一区块时操作幂等，不会报错。

## 5. 合约部署与 Gas 策略 (针对测试环境)

在 V1 的 Chaos 测试中，Gas 设置不当曾导致测试脚本挂起或交易失败。

*   **问题回顾**：Anvil 在高并发或特定状态下（如 Reorg 后），默认 Gas 估算可能失效或过低。
*   **Golang 迁移策略**：
    *   **显式资金储备**：在测试开始前（如 TestMain 或 Setup 阶段），利用 `anvil_setBalance` 给部署账户充值巨额 ETH，消除 Gas 耗尽风险。
    *   **Gas Limit 兜底**：在发送交易（尤其是部署合约）时，不要完全依赖 `EstimateGas`。建议在估算值基础上增加缓冲（如 * 1.2）或在私链环境下设置一个较高的固定 Gas Limit，确保测试稳定性优先于成本控制。



✦ There are the 5 lessons from the previous V1 codebase that I believe you can apply to the Golang version.


  1. Container Networking & RPC Access
      - Lesson: The V1 version failed to connect initially because it tried to access localhost:8545 from inside a Docker container.
      - Golang Strategy: Use service names (e.g., http://blockchain-node:8545) in your docker-compose.yml for inter-container
        communication. Ensure your Anvil/Geth node binds to 0.0.0.0 (not 127.0.0.1), or it will be unreachable from the Golang
        indexer container.


  2. Address Normalization (Case Sensitivity)
      - Lesson: EVM addresses can be checksummed (0xAbC...) or lowercased. V1 encountered bugs where data wasn't found due to case
        mismatches.
      - Golang Strategy: Enforce lowercase normalization immediately upon ingestion. When you json.Unmarshal or receive a
        common.Address from the RPC, convert it to a lowercase string before storing it in the DB or using it in business logic.


  3. Atomic Event Parsing & Persistence
      - Lesson: "Transactions not being stored" was caused by partial failures where blocks were saved but logs weren't, or getLogs
        timed out on large ranges.
      - Golang Strategy:
        - Pagination: Implement a loop that breaks down FilterLogs requests into small chunks (e.g., 100 blocks) to avoid RPC
          timeouts.
        - Atomicity: Use a single database transaction (tx) to save both the Block and all its Logs. If log parsing fails, roll back
          the block insertion to prevent "data gaps."


  4. Idempotency via Database Constraints
      - Lesson: Restarting the indexer caused crashes due to primary key conflicts on data that was already synced.
      - Golang Strategy: Rely on the database, not just code checks. Create a Composite Unique Index on (block_number, log_index)
        for events. Use ON CONFLICT DO NOTHING (UPSERT) in your SQL queries so that reprocessing the same block is safe and
        non-blocking.


  5. Gas Strategy for Test Environments
      - Lesson: Flaky tests in V1 were often due to accounts running out of ETH or bad gas estimation during heavy load/reorgs.
      - Golang Strategy: In your test setup (e.g., TestMain), explicitly use the anvil_setBalance RPC method to fund your deployer
        account with a massive amount of ETH. Don't rely solely on dynamic gas estimation for local tests; consider adding a safety
        buffer to gas limits.