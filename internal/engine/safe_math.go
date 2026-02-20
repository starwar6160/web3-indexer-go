package engine

import (
	"fmt"
	"math"
	"math/big"
)

// SafeCastUint64ToInt 将 uint64 安全转换为 int，防止溢出 (G115)
// 在 64 位系统中，int 通常是 64 位的，但在分布式或 32 位环境中需要严格校验。
func SafeCastUint64ToInt(val uint64) (int, error) {
	if val > math.MaxInt {
		return 0, fmt.Errorf("integer overflow: %d exceeds MaxInt", val)
	}
	return int(val), nil
}

// SafeCastUint64ToInt64 将 uint64 安全转换为 int64
func SafeCastUint64ToInt64(val uint64) (int64, error) {
	if val > math.MaxInt64 {
		return 0, fmt.Errorf("integer overflow: %d exceeds MaxInt64", val)
	}
	return int64(val), nil
}

// SafeCastInt64ToUint64 将 int64 安全转换为 uint64，防止负数转换错误
func SafeCastInt64ToUint64(val int64) (uint64, error) {
	if val < 0 {
		return 0, fmt.Errorf("integer underflow: %d is negative, cannot cast to uint64", val)
	}
	return uint64(val), nil
}

// BigIntToUint64 将 *big.Int 安全转换为 uint64
func BigIntToUint64(b *big.Int) (uint64, error) {
	if b == nil {
		return 0, fmt.Errorf("nil big.Int")
	}
	if b.Sign() < 0 {
		return 0, fmt.Errorf("negative big.Int: %s", b.String())
	}
	if b.IsUint64() {
		return b.Uint64(), nil
	}
	return 0, fmt.Errorf("big.Int overflow uint64: %s", b.String())
}

// MustBigIntToUint64 强制转换（用于已知安全的块高逻辑），若溢出则返回 0 或最大值
func MustBigIntToUint64(b *big.Int) uint64 {
	val, err := BigIntToUint64(b)
	if err != nil {
		// 在区块高度场景下，如果超过 uint64 基本上是链出问题了
		return 0
	}
	return val
}
