package wallet

import (
	"context"
	"crypto/ecdsa"
	"fmt"
	"math/big"
	"strings"
	"time"

	"spam-evm/types"

	ethereum "github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
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

func TransferFromFaucet(faucetWallet *types.Wallet, recipientKeys []string, amount *big.Int, maxConcurrent int) []error {
	var (
		balance *big.Int
		err     error
	)

	// Set context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Second)
	defer cancel()

	// Check faucet wallet balance with retries
	for i := 0; i < 3; i++ {
		balance, err = faucetWallet.Client.BalanceAt(ctx, faucetWallet.Address, nil)
		if err == nil {
			break
		}
		time.Sleep(time.Second * 2)
	}
	if err != nil {
		return []error{fmt.Errorf("failed to get faucet balance: %w", err)}
	}

	// Calculate total needed amount (including gas estimates)
	totalNeeded := new(big.Int).Mul(amount, big.NewInt(int64(len(recipientKeys))))
	gasEstimate := new(big.Int).Mul(big.NewInt(21000), big.NewInt(int64(len(recipientKeys)))) // 21000 is standard ETH transfer gas
	if balance.Cmp(new(big.Int).Add(totalNeeded, gasEstimate)) < 0 {
		return []error{fmt.Errorf("insufficient faucet balance for all transfers")}
	}

	// Initialize network parameters
	networkParams, err := types.NewNetworkParams(ctx, faucetWallet.Client)
	if err != nil {
		return []error{fmt.Errorf("failed to initialize network parameters: %w", err)}
	}

	// Create channels for concurrent processing
	type transferResult struct {
		index  int
		err    error
		txHash common.Hash
	}
	results := make(chan transferResult, len(recipientKeys))
	nonceChan := make(chan uint64, 1) // Channel to synchronize nonce updates

	// Initialize nonce
	initialNonce, err := faucetWallet.Client.PendingNonceAt(context.Background(), faucetWallet.Address)
	if err != nil {
		return []error{fmt.Errorf("failed to get initial nonce: %w", err)}
	}
	nonceChan <- initialNonce

	// Process transfers with concurrency limit
	sem := make(chan struct{}, maxConcurrent)
	for i, recipientKey := range recipientKeys {
		go func(index int, privateKey string) {
			sem <- struct{}{}        // Acquire semaphore
			defer func() { <-sem }() // Release semaphore

			// Create recipient wallet
			recipientWallet, err := NewWallet(privateKey, faucetWallet.Client, nil)
			if err != nil {
				results <- transferResult{index, fmt.Errorf("failed to create recipient wallet: %w", err), common.Hash{}}
				return
			}

			// Get and increment nonce atomically
			nonce := <-nonceChan
			nonceChan <- nonce + 1

			// Create and sign transaction
			tx := ethtypes.NewTransaction(nonce, recipientWallet.Address, amount, 21000, networkParams.GetGasPrice(), nil)
			signedTx, err := ethtypes.SignTx(tx, ethtypes.NewEIP155Signer(networkParams.GetChainID()), faucetWallet.PrivateKey)
			if err != nil {
				results <- transferResult{index, fmt.Errorf("failed to sign transaction: %w", err), common.Hash{}}
				return
			}

			// Send transaction
			err = faucetWallet.Client.SendTransaction(context.Background(), signedTx)
			if err != nil {
				results <- transferResult{index, fmt.Errorf("failed to send transaction: %w", err), common.Hash{}}
				return
			}

			// Store transaction hash for later confirmation
			txHash := signedTx.Hash()
			results <- transferResult{index, nil, txHash}
		}(i, recipientKey)
	}

	// Collect send results and prepare confirmation batch
	errors := make([]error, len(recipientKeys))
	pendingTxs := make(map[int]common.Hash)
	for i := 0; i < len(recipientKeys); i++ {
		result := <-results
		if result.err != nil {
			errors[result.index] = fmt.Errorf("transfer %d failed: %w", result.index, result.err)
		} else {
			pendingTxs[result.index] = result.txHash
		}
	}

	// Confirm transactions
	confirmResults := make(chan transferResult, len(pendingTxs))
	sem = make(chan struct{}, maxConcurrent) // Reuse semaphore for confirmation phase
	for idx, txHash := range pendingTxs {
		go func(index int, hash common.Hash) {
			sem <- struct{}{}        // Acquire semaphore
			defer func() { <-sem }() // Release semaphore

			// Wait for transaction receipt with timeout and retry
			var receipt *ethtypes.Receipt
			for range 5 { // 5 attempts, 2 second each
				ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				receipt, err = faucetWallet.Client.TransactionReceipt(ctx, hash)
				cancel()

				if err == nil {
					break
				}

				if err != ethereum.NotFound {
					confirmResults <- transferResult{index, fmt.Errorf("failed to get receipt: %w", err), hash}
					return
				}

				time.Sleep(2 * time.Second)
			}

			if receipt == nil {
				confirmResults <- transferResult{index, fmt.Errorf("transaction not mined within timeout"), hash}
				return
			}

			if receipt.Status == 0 {
				confirmResults <- transferResult{index, fmt.Errorf("transaction failed"), hash}
				return
			}

			confirmResults <- transferResult{index, nil, hash}
		}(idx, txHash)
	}

	// Collect confirmation results
	for range len(pendingTxs) {
		result := <-confirmResults
		if result.err != nil {
			errors[result.index] = fmt.Errorf("transfer %d confirmation failed: %w", result.index, result.err)
		}
	}

	return errors
}
