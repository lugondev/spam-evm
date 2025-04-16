# spam-evm

A command-line tool for stress testing EVM-compatible networks by sending multiple transactions from different wallets concurrently.

## Usage

You can configure spam-evm using either command-line flags or a YAML configuration file.

### Using Command-line Flags

```bash
# Using default spam command
spam-evm --provider-urls <comma-separated-urls> [flags]

# Using explicit spam subcommand
spam-evm spam --provider-urls <comma-separated-urls> [flags]
```

### Using YAML Configuration

The tool looks for a `config.yaml` file by default. You can also specify a different config file:

```bash
# Use default config.yaml with default command
spam-evm

# Use custom config file with spam subcommand
spam-evm spam --config <path-to-config-file>
```

A sample configuration file (`config.yaml`) format:

```yaml
# Number of transactions to send per wallet
txPerWallet: 10

# CPU core multiplier for GOMAXPROCS
cpuMultiplier: 5

# File path for private keys (one key per line)
keysFile: 'private-keys.txt'

# List of provider URLs (RPC endpoints)
providerUrls:
    - 'https://rpc1.example.com'
    - 'https://rpc2.example.com'
    - 'wss://ws.example.com'

# Faucet configuration
faucet:
    privateKey: '0x1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef'
    amountPerTransfer: '0.1' # Amount in ETH to transfer to each test wallet
```

See [example.config.yaml](example.config.yaml) for the latest configuration options.

### Command-line Flags

Global flags:
- `--config`: Path to YAML configuration file (default: config.yaml)

Configuration options (can be set via config file or command flags):
- `--provider-urls`: Comma-separated list of provider URLs. URLs will be randomly selected for load balancing.
- `--keys-file`: File path for private keys (one key per line)
- `--tx-per-wallet`: Number of transactions per wallet 
- `--cpu-multiplier`: CPU core multiplier for GOMAXPROCS. This affects:
  - Number of GOMAXPROCS (NumCPU * multiplier)
  - Connection pool size (optimized based on CPU cores and multiplier)
  - Maximum concurrent operations

Note: Command-line flags take precedence over values in the config file.

## Commands

### Spam Network

The `spam` command (which is also the default) sends multiple transactions from each wallet to stress test the network. It can be run either as the default command or explicitly using the `spam` subcommand:

```bash
# Using default command
spam-evm --config config.yaml

# Using explicit spam subcommand
spam-evm spam --config config.yaml
```

Features:

The default command sends multiple transactions from each wallet to stress test the network using:
- Transaction batching for improved throughput
- Parallel nonce management to prevent conflicts
- Mempool monitoring and throttling
- Dynamic batch retries with configurable timeouts
- Load balancing across multiple RPC endpoints

### Faucet Transfer

The `faucet` command transfers ETH from a faucet wallet to all test wallets specified in the keys file.

```bash
# Transfer ETH to all wallets in private-keys.txt
spam-evm faucet
```

## Examples

### Spam Network

Using command-line flags:
```bash
# Using multiple provider URLs
spam-evm --provider-urls="http://localhost:8545,http://localhost:8546,http://localhost:8547" --tx-per-wallet=20

# With custom keys file and CPU multiplier
spam-evm --provider-urls="http://node1:8545,http://node2:8545" --keys-file=keys.txt --cpu-multiplier=10
```

Using YAML configuration:
```bash
# Using a config file
spam-evm --config config.yaml

# Override config file values with flags
spam-evm --config config.yaml --tx-per-wallet=50
```

### Faucet Configuration

The faucet settings can be configured in the YAML config file:

```yaml
faucet:
  privateKey: '0x1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef'
  amountPerTransfer: '0.1' # Amount in ETH to transfer to each test wallet
```

## Private Keys File Format

The private keys file should contain one private key per line. Example:

```text
0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef
fedcba9876543210fedcba9876543210fedcba9876543210fedcba9876543210
```

## Performance Optimization

The tool implements several optimizations to maximize transaction throughput:

### Transaction Batching
- Groups multiple transactions into batches
- Configurable batch size (default: 100)
- Parallel batch processing
- Dynamic batch retries on failure

### Nonce Management
- Parallel nonce queue per wallet
- Pre-allocated nonce ranges
- Automatic nonce recovery and reuse
- Prevents nonce conflicts in high-throughput scenarios

### Connection Management
- Enhanced connection pooling
- Automatic health checks
- Connection load balancing
- Configurable pool size and retry settings

### Mempool Monitoring
- Tracks pending transaction count
- Implements adaptive throttling
- Prevents mempool overflow
- Configurable thresholds

## Metrics

The tool provides detailed performance metrics including:
- Connection time
- Transaction success rate
- Average transaction time
- Batch processing statistics
- Mempool utilization
- Total execution time
- Transactions per second (TPS)

## Load Balancing

The tool automatically distributes the load across all provided provider URLs by:
1. Randomly selecting a provider for each client connection
2. Creating multiple client connections for parallel transaction processing
3. Balancing the transaction load across available providers
4. Automatic failover on connection issues
