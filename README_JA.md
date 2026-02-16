# 産業用グレード Web3 イベントインデクサー (横浜ラボ)

[🌐 **English**](./README.md) | [🏮 **中文说明**](./README_ZH.md) | [🗾 **日本語の説明**](./README_JA.md)

### 🚀 ライブデモ
*   **本番環境 (Sepolia)**: [https://demo1.st6160.click/](https://demo1.st6160.click/)
*   **ローカルラボ (Anvil)**: [https://demo2.st6160.click/](https://demo2.st6160.click/)

**Go**、**PostgreSQL**、および **Docker** で構築された、極めて高い信頼性とコスト効率を誇るイーサリアムイベントインデクサーです。

## 🚀 技術的な特徴

*   **産業用グレードの信頼性**: 24時間 365日の無人稼働をサポート。**ブルーグリーンデプロイ (Staging-to-Production)** ワークフローを採用し、ダウンタイム 2秒未満の高速アップデートを実現。
*   **コスト最適化アーキテクチャ**: Alchemy と Infura の無料枠を最大限に活用するため、「**重み付きトークンバケット**」流量制限アルゴリズムを搭載（メイン/バックアップ比率 3:1）。
*   **429 自動サーキットブレーカー**: RPC ノードからの流量制限エラーを検知し、5分間の冷却期間と自動フェイルオーバーを即座に実行。
*   **決定論的なセキュリティ**: 起動時に **NetworkID/ChainID を強制検証**し、環境設定ミスによるデータベース汚染を物理的に防止。
*   **効率的なレンジスキャン**: `eth_getLogs` の一括処理（50ブロック/リクエスト）を最適化。**Keep-alive** プログレス機構により、イベントが少ない期間でも UI がリアルタイムに更新されます。
*   **Early-Bird API モード**: Web サーバーの起動をエンジン初期化から分離し、ミリ秒単位でポートを開放。コンテナ再起動時の Cloudflare 502 エラーを完全に排除。

## 🛠️ 技術スタック & ラボ環境

*   **バックエンド**: Go (Golang) + `go-ethereum`
*   **インフラ**: Docker (Demo/Sepolia/Debug 環境の物理的な分離)
*   **データベース**: PostgreSQL (各インスタンスに独立した物理データベースを割り当て)
*   **オブザーバビリティ**: Prometheus + Grafana (マルチ環境対応ダッシュボード)
*   **ハードウェア**: AMD Ryzen 7 3800X (8C/16T), 128GB DDR4 RAM, Samsung 990 PRO 4TB NVMe

## 📦 デプロイワークフロー

本プロジェクトでは、本番環境の安定性を確保するため「テスト駆動昇格」プロセスを採用しています：

1.  **テスト (Test)**: `make test-a1` (Sepolia) または `make test-a2` (Anvil) を使用して Staging ポート (8091/8092) にデプロイ。
2.  **検証 (Verify)**: Staging ポートで動作確認を実施。
3.  **昇格 (Promote)**: `make a1` または `make a2` を実行し、イメージを本番ポート (8081/8082) に瞬間的に反映。
    *   *仕組み*: `docker tag :latest -> :stable` + `docker compose up -d --no-build`。

## 📈 パフォーマンス指標

| モード | 対象ネットワーク | RPS 制限 | 遅延 | 戦略 |
| :--- | :--- | :--- | :--- | :--- |
| **安定版 (Stable)** | Sepolia (テストネット) | 3.5 RPS | ~12s | 重み付きマルチ RPC |
| **デモ版 (Demo)** | Anvil (ローカル) | 10000+ RPS | < 1s | 制限解除モード |
| **デバッグ版 (Debug)** | Sepolia (テストネット) | 5.0 RPS | ~12s | 商用 RPC 直結 |

## 🔐 セキュリティ ID

*   **開発者**: 周偉 (Zhou Wei) <zhouwei6160@gmail.com>
*   **ラボ所在地**: 神奈川県横浜市 (Yokohama, Japan)
*   **GPG フィンガープリント**: `FFA0 B998 E7AF 2A9A 9A2C  6177 F965 25FE 5857 5DCF`
*   **検証**: すべての API レスポンスは **Ed25519** で署名されており、データの完全性が保証されています。

---
© 2026 Zhou Wei. Yokohama Lab. All rights reserved.
