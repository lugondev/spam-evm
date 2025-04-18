package wallet

import (
	"context"
	"crypto/ecdsa"
	"fmt"
	"math/big"
	"strings"
	"sync"
	"time"

	"spam-evm/types"

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
)

// InitializeWallets optimizes wallet initialization by doing parallel nonce/balance fetching
func InitializeWallets(privateKeys []string, client *ethclient.Client) ([]*types.Wallet, error) {
	wallets := make([]*types.Wallet, len(privateKeys))
	var wg sync.WaitGroup
	errChan := make(chan error, len(privateKeys))

	// Process wallets in parallel
	for i, key := range privateKeys {
		wg.Add(1)
		go func(idx int, privKey string) {
			defer wg.Done()

			// Create basic wallet structure
			privKey = strings.TrimPrefix(privKey, "0x")
			privateKey, err := crypto.HexToECDSA(privKey)
			if err != nil {
				errChan <- fmt.Errorf("invalid private key at index %d: %w", idx, err)
				return
			}

			publicKey := privateKey.Public()
			publicKeyECDSA, ok := publicKey.(*ecdsa.PublicKey)
			if !ok {
				errChan <- fmt.Errorf("error casting public key to ECDSA at index %d", idx)
				return
			}

			address := crypto.PubkeyToAddress(*publicKeyECDSA)
			wallet := &types.Wallet{
				PrivateKey: privateKey,
				Address:    address,
				Client:     client,
				Balance:    big.NewInt(0),
			}

			// Get nonce and balance concurrently
			var nonceWg sync.WaitGroup
			nonceWg.Add(2)

			var nonceErr, balanceErr error

			// Get nonce
			go func() {
				defer nonceWg.Done()
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()

				nonce, err := client.PendingNonceAt(ctx, address)
				if err != nil {
					nonceErr = fmt.Errorf("failed to get nonce for %s: %w", address.Hex(), err)
					return
				}
				wallet.Nonce = nonce
			}()

			// Get balance
			go func() {
				defer nonceWg.Done()
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()

				balance, err := client.BalanceAt(ctx, address, nil)
				if err != nil {
					balanceErr = fmt.Errorf("failed to get balance for %s: %w", address.Hex(), err)
					return
				}
				wallet.Balance = balance
			}()

			nonceWg.Wait()

			if nonceErr != nil {
				errChan <- nonceErr
				return
			}
			if balanceErr != nil {
				errChan <- balanceErr
				return
			}

			wallets[idx] = wallet
		}(i, key)
	}

	wg.Wait()
	close(errChan)

	// Check for any errors
	var errors []error
	for err := range errChan {
		errors = append(errors, err)
	}
	if len(errors) > 0 {
		return nil, fmt.Errorf("wallet initialization errors: %v", errors)
	}

	return wallets, nil
}
