package pkg

import (
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

// GenerateRandomEthAddress generates a random Ethereum address
func GenerateRandomEthAddress() common.Address {
	key, err := crypto.GenerateKey()
	if err != nil {
		b := make([]byte, 20)
		for i := range b {
			b[i] = byte(i + 1)
		}
		return common.BytesToAddress(b)
	}
	return crypto.PubkeyToAddress(key.PublicKey)
}

// EthToWei converts ETH amount to Wei
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
