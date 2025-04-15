package pkg

import (
	"fmt"
	"math/big"
)

// EthToWei converts ETH amount string to Wei as big.Int
func EthToWei(ethAmount string) (*big.Int, error) {
	ethFloat, ok := new(big.Float).SetString(ethAmount)
	if !ok {
		return nil, fmt.Errorf("invalid ETH amount")
	}

	weiFactor := new(big.Float).SetFloat64(1e18) // 1 ETH = 1e18 Wei
	weiFloat := new(big.Float).Mul(ethFloat, weiFactor)

	wei := new(big.Int)
	weiFloat.Int(wei) // convert float to big.Int (truncates decimals)

	return wei, nil
}
