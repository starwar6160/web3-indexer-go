package engine

import (
	"math/big"
	mathrand "math/rand/v2"

	"github.com/ethereum/go-ethereum/common"
)

func (s *DeFiSimulator) generatePowerLawAmount(decimals int) *big.Int {
	expValue := mathrand.ExpFloat64()
	var magnitude float64
	switch {
	case expValue < 0.7:
		// #nosec G404
		magnitude = 1 + mathrand.Float64()*99
	case expValue < 0.95:
		// #nosec G404
		magnitude = 100 + mathrand.Float64()*9900
	default:
		// #nosec G404
		magnitude = 10000 + mathrand.Float64()*990000
	}

	amount := new(big.Float).SetInt64(int64(magnitude))
	amount.Mul(amount, big.NewFloat(1e18))
	targetPrecision := new(big.Float).SetInt(new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(decimals)), nil))
	amount.Quo(amount, targetPrecision)

	result := new(big.Int)
	amount.Int(result)
	return result
}

func (s *DeFiSimulator) generateLargeAmount(decimals int) *big.Int {
	// #nosec G404
	base := new(big.Float).SetFloat64(10000 + mathrand.Float64()*90000)
	precision := new(big.Float).SetInt(new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(decimals)), nil))
	base.Mul(base, precision)
	result := new(big.Int)
	base.Int(result)
	return result
}

func (s *DeFiSimulator) generateMegaAmount(decimals int) *big.Int {
	// #nosec G404
	base := new(big.Float).SetFloat64(100000 + mathrand.Float64()*900000)
	precision := new(big.Float).SetInt(new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(decimals)), nil))
	base.Mul(base, precision)
	result := new(big.Int)
	base.Int(result)
	return result
}

func (s *DeFiSimulator) generateMediumAmount(decimals int) *big.Int {
	// #nosec G404
	base := new(big.Float).SetFloat64(1000 + mathrand.Float64()*9000)
	precision := new(big.Float).SetInt(new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(decimals)), nil))
	base.Mul(base, precision)
	result := new(big.Int)
	base.Int(result)
	return result
}

func (s *DeFiSimulator) randomUserAddress() common.Address {
	addresses := []string{
		"0xf39Fd6e51aad88F6F4ce6aB8827279cffFb92266",
		"0x70997970C51812dc3A010C7d01b50e0d17dc79C8",
		"0x3C44CdDdB6a900fa2b585dd299e03d12FA4293BC",
		"0x90F79bf6EB2c4f870365E785982E1f101E93b906",
		"0x15d34AAf54267DB7D7c367839AAf71A00a2C6A65",
	}
	return common.HexToAddress(addresses[secureIntn(len(addresses))])
}
