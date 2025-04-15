package wallet

import (
	"fmt"
	"strings"

	"crypto/ecdsa"
	"spam-evm/types"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
)

func NewWallet(privateKeyHex string, client *ethclient.Client, metrics *types.PerformanceMetrics) (*types.Wallet, error) {
	privateKeyHex = strings.TrimPrefix(privateKeyHex, "0x")

	privateKey, err := crypto.HexToECDSA(privateKeyHex)
	if err != nil {
		return nil, fmt.Errorf("invalid private key: %w", err)
	}

	publicKey := privateKey.Public()
	publicKeyECDSA, ok := publicKey.(*ecdsa.PublicKey)
	if !ok {
		return nil, fmt.Errorf("error casting public key to ECDSA")
	}

	address := crypto.PubkeyToAddress(*publicKeyECDSA)

	return &types.Wallet{
		PrivateKey: privateKey,
		Address:    address,
		Client:     client,
	}, nil
}

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
