package engine

import "github.com/ethereum/go-ethereum/common"

var (
	// TransferEventHash: Transfer(address,address,uint256)
	TransferEventHash = common.HexToHash("0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f5514cfc0afda6")

	// ApprovalEventHash: Approval(address,address,uint256)
	ApprovalEventHash = common.HexToHash("0x8c5be1e5ebec7d5bd14f71427d1e84f3dd0314c0f7b2291e5b200ac8c7c3b925")

	// SwapEventHash (Uniswap V3): Swap(address,address,int256,int256,uint160,uint128,int24)
	SwapEventHash = common.HexToHash("0xc42079f94a6350d7e5735f2a1538197108a858e5111b9ad0a72f5db98e4c0388")

	// MintEventHash (Common ERC20/721 Mint)
	MintEventHash = common.HexToHash("0x0f6711612c5b94d76f9d34343434343434343434343434343434343434343434")
)
