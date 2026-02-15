# Project Architecture Diagram

This diagram represents the core components and data flow of the Web3 Indexer. You can copy the code below into [Excalidraw](https://excalidraw.com/) or any Mermaid-compatible viewer.

```mermaid
graph TD
    %% External Layer
    subgraph "External Sources"
        Anvil["Anvil (Local Node)"]
        ExternalRPC["External RPCs (Infura/QuickNode)"]
    end

    %% Infrastructure Layer
    subgraph "Core Engine (Go)"
        RPCPool["RPC Client Pool<br/>(Load Balancing & Failover)"]
        
        Fetcher["Fetcher (Concurrency Workers)"]
        Seq["Sequencer (Ordering & Reorg Detection)"]
        Proc["Processor (Business Logic)"]
        
        RPCPool -.->|JSON-RPC / Sub| Fetcher
        Fetcher -->|Channel: BlockData| Seq
        Seq -->|Ordered Batch| Proc
        Proc -->|Backpressure| Fetcher
    end

    %% Persistence & Security
    subgraph "Storage & Security"
        DB[(PostgreSQL)]
        Signer["Ed25519 Signer<br/>(Payload Authenticity)"]
        Metrics["Prometheus Metrics"]
    end

    %% Monitoring Layer
    subgraph "Dashboard & API"
        API["API Server<br/>(REST & WebSocket)"]
        Dash["Web Dashboard<br/>(Real-time Monitoring)"]
        Analyzer["Traffic Analyzer<br/>(Dynamic Anomaly Detection)"]
    end

    %% Flows
    Anvil --- RPCPool
    ExternalRPC --- RPCPool
    
    Proc -->|ACID Transaction| DB
    Proc -.->|Event Hook| API
    
    API -->|Read| DB
    API -->|Sign Payload| Signer
    API --- Analyzer
    
    Dash <-->|Secure WS / REST| API
    
    %% Metrics Monitoring
    Proc -.-> Metrics
    Fetcher -.-> Metrics
    API -.-> Metrics

    %% Styling
    style RPCPool fill:#f9f,stroke:#333,stroke-width:2px
    style Signer fill:#ff9,stroke:#333,stroke-width:2px
    style Analyzer fill:#9ff,stroke:#333,stroke-width:2px
```
