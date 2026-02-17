package models

import (
	"database/sql/driver"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/holiman/uint256"
)

// Uint256 封装 uint256.Int 以支持 sql.Scanner 和 driver.Valuer.
// 专为 EVM 链金额计算设计，避免精度丢失.
type Uint256 struct {
	*uint256.Int
}

func NewUint256(n uint64) Uint256 {
	return Uint256{uint256.NewInt(n)}
}

func NewUint256FromBigInt(b *big.Int) Uint256 {
	if b == nil {
		return Uint256{uint256.NewInt(0)}
	}
	u, overflow := uint256.FromBig(b)
	if overflow {
		// 处理溢出，返回最大值
		return Uint256{uint256.MustFromHex("0xffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff")}
	}
	return Uint256{u}
}

func NewUint256FromString(s string) (Uint256, bool) {
	u, err := uint256.FromDecimal(s)
	if err != nil {
		return Uint256{}, false
	}
	return Uint256{u}, true
}

// Value 实现 driver.Valuer (写入数据库).
func (u Uint256) Value() (driver.Value, error) {
	if u.Int == nil {
		return "0", nil
	}
	return u.Int.Dec(), nil
}

// Scan 实现 sql.Scanner (读取数据库).
func (u *Uint256) Scan(value interface{}) error {
	if value == nil {
		u.Int = uint256.NewInt(0)
		return nil
	}

	var s string
	switch v := value.(type) {
	case []byte:
		s = string(v)
	case string:
		s = v
	default:
		return fmt.Errorf("unsupported type for Uint256: %T", v)
	}

	// 处理科学计数法（PostgreSQL NUMERIC 可能返回）
	if strings.ContainsAny(s, "eE") {
		// 用 big.Float 解析科学计数法，再转 big.Int，最后转 uint256
		f, _, err := big.ParseFloat(s, 10, 0, big.ToNearestEven)
		if err != nil {
			return fmt.Errorf("failed to parse numeric %q: %w", s, err)
		}
		bi, acc := f.Int(nil)
		if acc != big.Exact {
			return fmt.Errorf("numeric %q is not an integer", s)
		}
		var overflow bool
		u.Int, overflow = uint256.FromBig(bi)
		if overflow {
			return fmt.Errorf("value %s overflows uint256", s)
		}
		return nil
	}

	// 普通十进制解析
	var err error
	u.Int, err = uint256.FromDecimal(s)
	if err != nil {
		return fmt.Errorf("failed to convert %s to Uint256: %w", s, err)
	}
	return nil
}

// String 返回十进制字符串表示.
func (u Uint256) String() string {
	if u.Int == nil {
		return "0"
	}
	return u.Int.Dec()
}

// 以下保留 BigInt 兼容性，推荐新项目使用 Uint256.

// BigInt 封装 math/big.Int 以支持 sql.Scanner 和 driver.Valuer.
// 它可以自动处理 Go BigInt <-> Postgres NUMERIC 的转换.
type BigInt struct {
	*big.Int
}

func NewBigInt(n int64) BigInt {
	return BigInt{big.NewInt(n)}
}

func NewBigIntFromString(s string) (BigInt, bool) {
	i, ok := new(big.Int).SetString(s, 10)
	return BigInt{i}, ok
}

// Value 实现 driver.Valuer (写入数据库).
func (b BigInt) Value() (driver.Value, error) {
	if b.Int == nil {
		return "0", nil
	}
	return b.Int.String(), nil
}

// Scan 实现 sql.Scanner (读取数据库).
func (b *BigInt) Scan(value interface{}) error {
	if value == nil {
		b.Int = new(big.Int)
		return nil
	}
	switch v := value.(type) {
	case []byte:
		s := string(v)
		// 支持 hex 字符串 (0x...)
		if len(s) >= 2 && s[0] == '0' && (s[1] == 'x' || s[1] == 'X') {
			i, ok := new(big.Int).SetString(s[2:], 16)
			if !ok {
				return fmt.Errorf("failed to convert hex %s to BigInt", s)
			}
			b.Int = i
			return nil
		}
		// 处理科学计数法（PostgreSQL NUMERIC 可能返回）
		if strings.ContainsAny(s, "eE") {
			f, _, err := big.ParseFloat(s, 10, 0, big.ToNearestEven)
			if err != nil {
				return fmt.Errorf("failed to parse numeric %q: %w", s, err)
			}
			bi, acc := f.Int(nil)
			if acc != big.Exact {
				return fmt.Errorf("numeric %q is not an integer", s)
			}
			b.Int = bi
			return nil
		}
		i, ok := new(big.Int).SetString(s, 10)
		if !ok {
			return fmt.Errorf("failed to convert %s to BigInt", s)
		}
		b.Int = i
	case string:
		// 支持 hex 字符串 (0x...)
		if len(v) >= 2 && v[0] == '0' && (v[1] == 'x' || v[1] == 'X') {
			i, ok := new(big.Int).SetString(v[2:], 16)
			if !ok {
				return fmt.Errorf("failed to convert hex %s to BigInt", v)
			}
			b.Int = i
			return nil
		}
		// 处理科学计数法（PostgreSQL NUMERIC 可能返回）
		if strings.ContainsAny(v, "eE") {
			f, _, err := big.ParseFloat(v, 10, 0, big.ToNearestEven)
			if err != nil {
				return fmt.Errorf("failed to parse numeric %q: %w", v, err)
			}
			bi, acc := f.Int(nil)
			if acc != big.Exact {
				return fmt.Errorf("numeric %q is not an integer", v)
			}
			b.Int = bi
			return nil
		}
		i, ok := new(big.Int).SetString(v, 10)
		if !ok {
			return fmt.Errorf("failed to convert %s to BigInt", v)
		}
		b.Int = i
	case int64:
		b.Int = big.NewInt(v)
	case int:
		b.Int = big.NewInt(int64(v))
	default:
		return fmt.Errorf("unsupported type for BigInt: %T", v)
	}
	return nil
}

// 对应数据库的结构体
type Block struct {
	ProcessedAt      time.Time `db:"processed_at"`
	Number           BigInt    `db:"number"`
	Hash             string    `db:"hash"`
	ParentHash       string    `db:"parent_hash"`
	Timestamp        uint64    `db:"timestamp"`
	GasLimit         uint64    `db:"gas_limit"`
	GasUsed          uint64    `db:"gas_used"`
	BaseFeePerGas    *BigInt   `db:"base_fee_per_gas"`
	TransactionCount int       `db:"transaction_count"`
}

type Transfer struct {
	BlockNumber  BigInt  `db:"block_number"`
	TxHash       string  `db:"tx_hash"`
	LogIndex     uint    `db:"log_index"`
	From         string  `db:"from_address"`
	To           string  `db:"to_address"`
	TokenAddress string  `db:"token_address"`
	Symbol       string  `db:"symbol"`        // ✅ 代币符号（如 USDC, USDT）
	Type         string  `db:"activity_type"` // ✅ 活动类型（如 TRANSFER, SWAP, MINT）
	Amount       Uint256 `db:"amount"`        // 使用 Uint256 保证金融级精度
}
