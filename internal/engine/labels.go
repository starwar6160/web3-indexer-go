package engine

import "strings"

// FaucetLabels 定义测试网“发钱大户”地址标签
var FaucetLabels = map[string]string{
	"0x6Cc9397c3B38739daCbfaA18E600263a1174457D": "Alchemy Faucet",
	"0x8ad9277301013d084931fc5ca31787b122fca66":  "Infura Faucet",
	"0x4b78662286D9D99772a93c091910609614444444": "RockX Faucet",
	"0x2e6B6A8625686868686868686868686868686868": "Google Faucet",
}

// GetAddressLabel 返回已知实体的标签，若未知则返回截断地址
func GetAddressLabel(addr string) string {
	for k, v := range FaucetLabels {
		if strings.EqualFold(k, addr) {
			return v
		}
	}
	return ""
}
