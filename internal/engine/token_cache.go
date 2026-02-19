package engine

import (
	"container/list"
	"log/slog"
	"sync"
)

// TokenInfo 代币元数据
type TokenInfo struct {
	Symbol   string // 代币符号（如 USDT）
	Decimals int    // 小数位数
	Name     string // 代币全称
}

// tokenCache LRU 缓存（最近最少使用淘汰）
type tokenCache struct {
	mu       sync.RWMutex
	capacity int                       // 最大缓存数量
	items    map[string]*list.Element // address -> *list.Element
	lru      *list.List                // LRU 队列

	// 统计
	hits   int64
	misses int64
}

type cacheItem struct {
	address string
	info    TokenInfo
}

// NewTokenCache 创建代币缓存
// capacity: 建议缓存 100,000 个活跃代币（16G 内存中可忽略）
func NewTokenCache(capacity int) *tokenCache {
	return &tokenCache{
		capacity: capacity,
		items:    make(map[string]*list.Element),
		lru:      list.New(),
	}
}

// Get 获取代币信息
func (c *tokenCache) Get(address string) (TokenInfo, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if elem, ok := c.items[address]; ok {
		c.hits++
		// 移到队列头部（表示最近使用）
		c.lru.MoveToFront(elem)
		return elem.Value.(*cacheItem).info, true
	}

	c.misses++
	return TokenInfo{}, false
}

// Set 设置代币信息
func (c *tokenCache) Set(address string, info TokenInfo) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// 如果已存在，更新并移到头部
	if elem, ok := c.items[address]; ok {
		c.lru.MoveToFront(elem)
		elem.Value.(*cacheItem).info = info
		return
	}

	// 添加新项
	item := &cacheItem{address: address, info: info}
	elem := c.lru.PushFront(item)
	c.items[address] = elem

	// 检查是否超过容量
	if c.lru.Len() > c.capacity {
		// 淘汰最久未使用的项
		oldest := c.lru.Back()
		if oldest != nil {
			c.lru.Remove(oldest)
			delete(c.items, oldest.Value.(*cacheItem).address)
		}
	}
}

// GetStats 获取缓存统计
func (c *tokenCache) GetStats() map[string]interface{} {
	c.mu.RLock()
	defer c.mu.RUnlock()

	total := c.hits + c.misses
	hitRate := 0.0
	if total > 0 {
		hitRate = float64(c.hits) / float64(total) * 100.0
	}

	return map[string]interface{}{
		"capacity":   c.capacity,
		"size":       c.lru.Len(),
		"hits":       c.hits,
		"misses":     c.misses,
		"hit_rate":   hitRate,
		"memory_mb":  c.lru.Len() * 500 / 1024 / 1024, // 估算：每个 TokenInfo ~500 bytes
	}
}

// Clear 清空缓存
func (c *tokenCache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.items = make(map[string]*list.Element)
	c.lru.Init()
	c.hits = 0
	c.misses = 0
	
	slog.Info("🗑️ Token Cache Cleared")
}

// 全局单例
var globalTokenCache *tokenCache

// InitTokenCache 初始化全局缓存
func InitTokenCache(capacity int) {
	globalTokenCache = NewTokenCache(capacity)
	slog.Info("💾 Token Cache Initialized", "capacity", capacity)
}

// GetTokenInfo 获取代币信息（优先从缓存）
func GetTokenInfo(address string) (TokenInfo, bool) {
	if globalTokenCache != nil {
		return globalTokenCache.Get(address)
	}
	return TokenInfo{}, false
}

// SetTokenInfo 设置代币信息
func SetTokenInfo(address string, info TokenInfo) {
	if globalTokenCache != nil {
		globalTokenCache.Set(address, info)
	}
}

// GetTokenCacheStats 获取缓存统计（用于 API）
func GetTokenCacheStats() map[string]interface{} {
	if globalTokenCache != nil {
		return globalTokenCache.GetStats()
	}
	return map[string]interface{}{"error": "cache not initialized"}
}
