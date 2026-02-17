package engine

import (
	"crypto/rand"
	"math/big"
)

// secureIntn 返回 [0, n) 之间的安全随机整数
func secureIntn(n int) int {
	if n <= 0 {
		return 0
	}
	val, err := rand.Int(rand.Reader, big.NewInt(int64(n)))
	if err != nil {
		return 0
	}
	return int(val.Int64())
}
