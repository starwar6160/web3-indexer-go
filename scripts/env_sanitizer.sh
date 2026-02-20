#!/bin/bash
# 🔥 环境配置自检脚本 (env_sanitizer.sh)
# 在容器启动前自动检测 RPC 环境并强制设定 APP_MODE
# 防止 Anvil/Testnet 身份误判

set -e

# 颜色输出
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

log_info() {
    echo -e "${BLUE}[INFO]${NC} $1"
}

log_warn() {
    echo -e "${YELLOW}[WARN]${NC} $1"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

log_success() {
    echo -e "${GREEN}[OK]${NC} $1"
}

# 检测 RPC 端点是否可连接
test_rpc_endpoint() {
    local url=$1
    local timeout=${2:-3}
    
    # 提取主机和端口
    if [[ $url =~ http://([^:/]+):([0-9]+) ]] || [[ $url =~ https://([^:/]+):([0-9]+) ]]; then
        local host="${BASH_REMATCH[1]}"
        local port="${BASH_REMATCH[2]}"
        
        # 尝试连接
        if timeout $timeout bash -c "cat < /dev/null > /dev/tcp/$host/$port" 2>/dev/null; then
            return 0
        fi
    fi
    
    # 使用 curl 作为备选
    if command -v curl &> /dev/null; then
        if curl -sf -m $timeout -X POST \
            -H "Content-Type: application/json" \
            -d '{"jsonrpc":"2.0","method":"eth_chainId","params":[],"id":1}' \
            "$url" &>/dev/null; then
            return 0
        fi
    fi
    
    return 1
}

# 获取 Chain ID
get_chain_id() {
    local url=$1
    local response
    
    if command -v curl &> /dev/null; then
        response=$(curl -sf -m 3 -X POST \
            -H "Content-Type: application/json" \
            -d '{"jsonrpc":"2.0","method":"eth_chainId","params":[],"id":1}' \
            "$url" 2>/dev/null | grep -o '"result":"0x[^"]*"' | cut -d'"' -f4)
        
        if [[ -n "$response" ]]; then
            # 转换 16 进制到 10 进制
            printf "%d\n" "$response" 2>/dev/null || echo ""
        fi
    fi
}

# 主检测逻辑
main() {
    log_info "🔍 环境配置自检启动..."
    
    # 检查当前 APP_MODE
    current_mode="${APP_MODE:-}"
    if [[ -n "$current_mode" ]]; then
        log_info "检测到已设置 APP_MODE=$current_mode"
    fi
    
    # 获取 RPC URL
    rpc_url="${RPC_URL:-}"
    rpc_urls="${RPC_URLS:-}"
    
    # 优先使用 RPC_URL，如果没有则取 RPC_URLS 的第一个
    target_url="$rpc_url"
    if [[ -z "$target_url" && -n "$rpc_urls" ]]; then
        target_url=$(echo "$rpc_urls" | cut -d',' -f1 | tr -d ' ')
        log_info "从 RPC_URLS 提取第一个端点: $target_url"
    fi
    
    # 如果没有 RPC URL，尝试默认 Anvil 端口
    if [[ -z "$target_url" ]]; then
        log_warn "未检测到 RPC_URL 或 RPC_URLS，尝试默认 Anvil 端口..."
        
        # 尝试常见的 Anvil 端口
        for port in 8545 8546 8555; do
            if test_rpc_endpoint "http://localhost:$port" 2; then
                target_url="http://localhost:$port"
                log_success "检测到 Anvil 在默认端口 $port"
                break
            fi
        done
    fi
    
    # 如果还是没有，强制设置为 Anvil 模式（demo2 环境）
    if [[ -z "$target_url" ]]; then
        log_warn "⚠️  无法检测到活跃的 RPC 端点"
        
        # 检查是否在容器环境中
        if [[ -f "/.dockerenv" ]] || [[ -n "${KUBERNETES_SERVICE_HOST:-}" ]]; then
            log_info "检测到容器环境，强制设置为 EPHEMERAL_ANVIL 模式"
            export APP_MODE="EPHEMERAL_ANVIL"
            echo "export APP_MODE=EPHEMERAL_ANVIL"
            exit 0
        fi
        
        log_error "❌ 无法确定运行环境，请手动设置 APP_MODE"
        exit 1
    fi
    
    log_info "目标 RPC 端点: $target_url"
    
    # 检测 Anvil 特征
    is_anvil=false
    
    # 1. URL 特征检测
    if [[ "$target_url" =~ localhost ]] || 
       [[ "$target_url" =~ 127\.0\.0\.1 ]] ||
       [[ "$target_url" =~ :8545 ]] ||
       [[ "$target_url" =~ :8546 ]] ||
       [[ "$target_url" =~ anvil ]]; then
        log_info "URL 特征符合 Anvil 模式"
        is_anvil=true
    fi
    
    # 2. 检测 Chain ID
    chain_id=$(get_chain_id "$target_url")
    if [[ -n "$chain_id" ]]; then
        log_info "检测到 Chain ID: $chain_id"
        
        case "$chain_id" in
            31337)
                log_success "✅ Chain ID 31337 确认是 Anvil 本地网络"
                is_anvil=true
                ;;
            1)
                log_warn "⚠️  Chain ID 1 是以太坊主网"
                is_anvil=false
                ;;
            11155111)
                log_warn "⚠️  Chain ID 11155111 是 Sepolia 测试网"
                is_anvil=false
                ;;
            *)
                log_info "未知 Chain ID: $chain_id"
                ;;
        esac
    else
        log_warn "无法获取 Chain ID，依赖 URL 特征判断"
    fi
    
    # 3. 检测响应速度（Anvil 本地通常 < 10ms）
    if command -v curl &> /dev/null; then
        start_time=$(date +%s%N)
        if curl -sf -m 2 -X POST \
            -H "Content-Type: application/json" \
            -d '{"jsonrpc":"2.0","method":"eth_blockNumber","params":[],"id":1}' \
            "$target_url" &>/dev/null; then
            end_time=$(date +%s%N)
            elapsed_ms=$(( (end_time - start_time) / 1000000 ))
            
            if [[ $elapsed_ms -lt 50 ]]; then
                log_info "RPC 响应极快 (${elapsed_ms}ms)，符合本地 Anvil 特征"
                is_anvil=true
            fi
        fi
    fi
    
    # 最终决策
    if [[ "$is_anvil" == true ]]; then
        log_success "🚀 识别为 Anvil 本地环境"
        
        # 检查当前模式是否正确
        if [[ "$current_mode" != "EPHEMERAL_ANVIL" ]]; then
            if [[ -n "$current_mode" ]]; then
                log_warn "⚠️  当前 APP_MODE=$current_mode 与检测结果不符！"
                log_warn "    强制覆盖为 EPHEMERAL_ANVIL"
            else
                log_info "设置 APP_MODE=EPHEMERAL_ANVIL"
            fi
            
            export APP_MODE="EPHEMERAL_ANVIL"
            echo "export APP_MODE=EPHEMERAL_ANVIL"
        else
            log_success "APP_MODE 已正确设置为 EPHEMERAL_ANVIL"
        fi
        
        # 输出调优建议
        echo ""
        log_info "💡 调优建议:"
        log_info "   - 使用 Beast 模式: curl -X POST http://localhost:8082/debug/hotune/preset -d '{\"mode\":\"BEAST\"}'"
        log_info "   - 查看状态: curl http://localhost:8082/debug/snapshot"
        
    else
        log_info "🛡️ 识别为测试网/生产环境"
        
        if [[ "$current_mode" != "PERSISTENT_TESTNET" ]]; then
            if [[ -n "$current_mode" ]]; then
                log_warn "⚠️  当前 APP_MODE=$current_mode 与检测结果不符！"
                log_warn "    强制覆盖为 PERSISTENT_TESTNET"
            else
                log_info "设置 APP_MODE=PERSISTENT_TESTNET"
            fi
            
            export APP_MODE="PERSISTENT_TESTNET"
            echo "export APP_MODE=PERSISTENT_TESTNET"
        else
            log_success "APP_MODE 已正确设置为 PERSISTENT_TESTNET"
        fi
        
        echo ""
        log_info "💡 保守模式建议:"
        log_info "   - 系统将使用 2 QPS 的谨慎限流"
        log_info "   - 背压阈值设置为 100 块"
    fi
    
    log_success "环境自检完成"
    exit 0
}

# 如果脚本被直接执行（不是被 source）
if [[ "${BASH_SOURCE[0]}" == "${0}" ]]; then
    main "$@"
fi
