package engine

import (
	"log/slog"
	"sync"
)

// AddressManager 本地地址标签管理器
// 设计理念：用本地内存置换昂贵的 RPC 调用
// 16G 内存足够存储数百万条地址标签
type AddressManager struct {
	labels sync.Map // map[address]label
	stats  struct {
		hits   int64
		misses int64
	}
}

// AddressLabel 地址标签信息
type AddressLabel struct {
	Name        string // 显示名称
	Type        string // 标签类型
	Icon        string // 图标（用于 UI）
	Description string // 描述
}

// NewAddressManager 创建地址管理器
func NewAddressManager() *AddressManager {
	am := &AddressManager{}
	am.loadInitialLabels()
	return am
}

// loadInitialLabels 加载核心地址标签
func (am *AddressManager) loadInitialLabels() {
	// 核心地址
	coreAddresses := map[string]AddressLabel{
		// ========== 特殊地址 ==========
		"0x0000000000000000000000000000000000000000": {
			Name:        "Null Address",
			Type:        "special",
			Icon:        "🔥",
			Description: "Burn address",
		},
		"0x000000000000000000000000000000000000dead": {
			Name:        "EOA Killed",
			Type:        "special",
			Icon:        "💀",
			Description: "Destroyed account",
		},

		// ========== 交易所 ==========
		"0x71C7656EC7ab88b098defB751B7401B5f6d8976F": {
			Name:        "Binance: Cold Wallet",
			Type:        "exchange",
			Icon:        "🏦",
			Description: "Binance cold storage",
		},
		"0x28C6c06298d514Db089934071355E5743bf21d60": {
			Name:        "Binance: Hot Wallet",
			Type:        "exchange",
			Icon:        "💸",
			Description: "Binance hot wallet",
		},
		"0xD99D1c33F9fC3444f8101754aBC46c52416550D1": {
			Name:        "Coinbase",
			Type:        "exchange",
			Icon:        "🏛️",
			Description: "Coinbase wallet",
		},
		"0x5041Ed7590dA95bdf9a4BDB72fd194A19F5b7B51": {
			Name:        "Kraken",
			Type:        "exchange",
			Icon:        "🐙",
			Description: "Kraken exchange",
		},

		// ========== DeFi 协议 ==========
		"0x1f9840a85d5af5bf1d1762f925bdaddc4201f984": {
			Name:        "Uniswap V3: Factory",
			Type:        "defi",
			Icon:        "🦄",
			Description: "Uniswap V3 factory contract",
		},
		"0x68b3465833fb72A70ecDF485E0e4C7bD8665Fc45": {
			Name:        "Uniswap V2: Router",
			Type:        "defi",
			Icon:        "🦄",
			Description: "Uniswap V2 router",
		},
		"0xE592427A0AEce92De3Edee1F18E0157C05861564": {
			Name:        "Uniswap V3: Router",
			Type:        "defi",
			Icon:        "🦄",
			Description: "Uniswap V3 router",
		},
		"0x7d2768dE32b0b80b7a3454c06BdAc94A69DDc7A9": {
			Name:        "Aave: Lending Pool",
			Type:        "defi",
			Icon:        "🏦",
			Description: "Aave V2 lending pool",
		},
		"0x8dFf5E27EA6b7AC08EbFdf9eB1450B85D7F75Fa6": {
			Name:        "Curve: Factory",
			Type:        "defi",
			Icon:        "🌀",
			Description: "Curve factory",
		},
		"0xC36442b4a4522E871399Cd717aBDD847Ab11FE88": {
			Name:        "OpenSea: Seaport",
			Type:        "nft",
			Icon:        "🖼️",
			Description: "OpenSea NFT marketplace",
		},

		// ========== 稳定币 ==========
		"0xdAC17F958D2ee523a2206206994597C13D831ec7": {
			Name:        "Tether: USDT",
			Type:        "stablecoin",
			Icon:        "💵",
			Description: "Tether US dollar",
		},
		"0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48": {
			Name:        "Centre: USDC",
			Type:        "stablecoin",
			Icon:        "💵",
			Description: "USD Coin",
		},
		"0x6B175474E89094C44Da98b954EedeAC495271d0F": {
			Name:        "Dai Stablecoin",
			Type:        "stablecoin",
			Icon:        "💴",
			Description: "Dai stablecoin",
		},
		"0x4F3AfEC4E5a3F2A6d1D48D8FE94bEe68F6a710a6": {
			Name:        "Binance USD",
			Type:        "stablecoin",
			Icon:        "💵",
			Description: "Binance USD stablecoin",
		},

		// ========== WETH ==========
		"0xC02aaA39b223FE8D0A0e5C4F27eAD9083C756Cc2": {
			Name:        "Wrapped Ether",
			Type:        "token",
			Icon:        "🔷",
			Description: "WETH - Wrapped Ether",
		},

		// ========== 跨链桥 ==========
		"0x3c44cddd502bb20500c75a03d6123133aeec10c4": {
			Name:        "Polygon: Bridge",
			Type:        "bridge",
			Icon:        "🌉",
			Description: "Polygon bridge",
		},
		"0x88E09A0Ea047217831299A6cE27435a1f72DDa1F": {
			Name:        "Hop Protocol",
			Type:        "bridge",
			Icon:        "🌉",
			Description: "Hop bridge",
		},

		// ========== 著名地址（巨鲸） ==========
		"0xAb5801a7D398351b8bE11C439e05C5B3259aeC9B": {
			Name:        "MistEvaluator: NFT Collector",
			Type:        "whale",
			Icon:        "🐋",
			Description: "Famous NFT collector",
		},
		"0x46B3Ad0789646C13B8Dc1f891fB639bDAAb1cfE4": {
			Name:        "MeebitsWhale",
			Type:        "whale",
			Icon:        "🐋",
			Description: "NFT whale",
		},
	}

	// 加载到内存
	count := 0
	for addr, label := range coreAddresses {
		labelCopy := label
		am.labels.Store(normalizeAddress(addr), &labelCopy)
		count++
	}

	slog.Info("🏷️ Address Labels Loaded", "count", count, "type", "core_addresses")
}

// Identify 识别地址标签（零 RPC 消耗）
func (am *AddressManager) Identify(address string) *AddressLabel {
	normalized := normalizeAddress(address)

	if label, ok := am.labels.Load(normalized); ok {
		am.stats.hits++
		if l, ok := label.(*AddressLabel); ok {
			return l
		}
	}

	am.stats.misses++
	return nil
}

// GetLabel 获取标签字符串（用于 UI 显示）
func (am *AddressManager) GetLabel(address string) string {
	if label := am.Identify(address); label != nil {
		return label.Name
	}

	// 降级处理：返回 0x1234...5678 格式
	if len(address) > 10 {
		return address[:6] + "..." + address[len(address)-4:]
	}
	return address
}

// GetLabelWithIcon 获取带图标的标签
func (am *AddressManager) GetLabelWithIcon(address string) string {
	if label := am.Identify(address); label != nil {
		return label.Icon + " " + label.Name
	}
	return am.GetLabel(address)
}

// GetStats 获取统计信息
func (am *AddressManager) GetStats() map[string]interface{} {
	total := am.stats.hits + am.stats.misses
	hitRate := 0.0
	if total > 0 {
		hitRate = float64(am.stats.hits) / float64(total) * 100.0
	}

	return map[string]interface{}{
		"total_labels": am.getLabelCount(),
		"hits":         am.stats.hits,
		"misses":       am.stats.misses,
		"hit_rate":     hitRate,
	}
}

// getLabelCount 获取标签总数
func (am *AddressManager) getLabelCount() int {
	count := 0
	am.labels.Range(func(_, _ interface{}) bool {
		count++
		return true
	})
	return count
}

// normalizeAddress 标准化地址（转小写）
func normalizeAddress(addr string) string {
	if len(addr) >= 2 && addr[0] == '0' && (addr[1] == 'x' || addr[1] == 'X') {
		// 转小写
		result := make([]byte, len(addr))
		for i := 0; i < len(addr); i++ {
			c := addr[i]
			if c >= 'A' && c <= 'F' {
				c += 32
			}
			result[i] = c
		}
		return string(result)
	}
	return addr
}

// AddLabel 动态添加标签（用于运行时学习）
func (am *AddressManager) AddLabel(address string, label AddressLabel) {
	normalized := normalizeAddress(address)
	am.labels.Store(normalized, &label)
	slog.Debug("🏷️ New label learned", "address", normalized[:10]+"...", "label", label.Name)
}
