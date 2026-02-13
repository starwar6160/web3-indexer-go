package engine

// This file serves as the main entry point for the RPCClientPool module.
// The implementation has been split into multiple files for better maintainability:
// - rpc_pool_core.go: Core RPC pool struct and initialization
// - rpc_pool_requests.go: Request methods (BlockByNumber, HeaderByNumber, etc.)
// - rpc_pool_health.go: Health check and management methods
//
// This allows for better organization and easier maintenance of the codebase.
