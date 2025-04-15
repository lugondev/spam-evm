# spam-evm

A command-line tool for stress testing EVM-compatible networks by sending multiple transactions from different wallets concurrently.

## Usage

```bash
spam-evm --provider-urls <comma-separated-urls> [flags]
```

### Required Flags

- `--provider-urls`: Comma-separated list of provider URLs. URLs will be randomly selected for load balancing.

### Optional Flags

- `--keys-file`: File path for private keys (one key per line, default: private-keys.txt)
- `--tx-per-wallet`: Number of transactions per wallet (default: 10)
- `--cpu-multiplier`: CPU core multiplier for GOMAXPROCS (default: 5)

## Example

```bash
# Using multiple provider URLs
spam-evm --provider-urls="http://localhost:8545,http://localhost:8546,http://localhost:8547" --tx-per-wallet=20

# With custom keys file and CPU multiplier
spam-evm --provider-urls="http://node1:8545,http://node2:8545" --keys-file=keys.txt --cpu-multiplier=10
```

## Private Keys File Format

The private keys file should contain one private key per line. Example:

```text
0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef
fedcba9876543210fedcba9876543210fedcba9876543210fedcba9876543210
```

## Metrics

The tool provides performance metrics including:
- Connection time
- Transaction success rate
- Average transaction time
- Total execution time

## Load Balancing

The tool automatically distributes the load across all provided provider URLs by:
1. Randomly selecting a provider for each client connection
2. Creating multiple client connections for parallel transaction processing
3. Balancing the transaction load across available providers
