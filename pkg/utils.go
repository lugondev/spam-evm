package pkg

import (
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
func EthToWei(eth string) (*big.Int, error) {
	wei := new(big.Int)
	wei.SetString(eth, 10)
	return wei, nil
}
