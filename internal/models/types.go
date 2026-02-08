package models

import (
	"database/sql/driver"
	"fmt"
	"math/big"
)

// BigInt 封装 math/big.Int 以支持 sql.Scanner 和 driver.Valuer
// 它可以自动处理 Go BigInt <-> Postgres NUMERIC 的转换
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

// Value 实现 driver.Valuer (写入数据库)
func (b BigInt) Value() (driver.Value, error) {
	if b.Int == nil {
		return "0", nil
	}
	return b.Int.String(), nil
}

// Scan 实现 sql.Scanner (读取数据库)
func (b *BigInt) Scan(value interface{}) error {
	if value == nil {
		b.Int = new(big.Int)
		return nil
	}
	switch v := value.(range) {
	case []byte:
		i, ok := new(big.Int).SetString(string(v), 10)
		if !ok {
			return fmt.Errorf("failed to convert %s to BigInt", string(v))
		}
		b.Int = i
	default:
		return fmt.Errorf("unsupported type for BigInt: %T", v)
	}
	return nil
}

// 对应数据库的结构体
type Block struct {
	Number     BigInt `db:"number"` 
	Hash       string `db:"hash"` 
	ParentHash string `db:"parent_hash"` 
	Timestamp  uint64 `db:"timestamp"` 
}

type Transfer struct {
	BlockNumber  BigInt `db:"block_number"` 
	TxHash       string `db:"tx_hash"` 
	LogIndex     uint   `db:"log_index"` 
	From         string `db:"from_address"` 
	To           string `db:"to_address"` 
	Amount       BigInt `db:"amount"` 
	TokenAddress string `db:"token_address"` 
}
