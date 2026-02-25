package engine

import (
	"crypto/rand"
	"math/big"
)

// SafeInt64Diff 提供了溢出安全的 uint64 差值计算并转换为 int64
// 用于处理区块链高度（uint64）在 Lag 计算时的边界问题
func SafeInt64Diff(a, b uint64) int64 {
	if a >= b {
		diff := a - b
		if diff > 9223372036854775807 { // int64 max
			return 9223372036854775807
		}
		return int64(diff)
	}

	diff := b - a
	if diff > 9223372036854775808 { // abs(int64 min)
		return -9223372036854775808
	}
	return -int64(diff)
}

// SafeUint64ToInt64 安全地将 uint64 转换为 int64
func SafeUint64ToInt64(v uint64) int64 {
	const maxInt64 = uint64(9223372036854775807)
	if v > maxInt64 {
		return 9223372036854775807
	}
	return int64(v)
}

// secureIntn 生成一个安全的随机整数 [0, n)
func secureIntn(n int) int {
	if n <= 0 {
		return 0
	}
	res, err := rand.Int(rand.Reader, big.NewInt(int64(n)))
	if err != nil {
		return 0
	}
	return int(res.Int64())
}
