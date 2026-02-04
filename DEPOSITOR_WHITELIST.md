# Depositor Whitelist Filter Implementation

## Overview

This implementation adds a depositor address whitelist filter to the CCTP relayer. The filter only processes messages from whitelisted depositor addresses on EVM chains (Ethereum, Avalanche, Optimism, Arbitrum). The whitelist is dynamically fetched from QuickNode's KV store REST API and cached with periodic refresh.

## Architecture

The filter is integrated into the message processing pipeline in `cmd/process.go`, alongside existing filters:

1. `FilterDisabledCCTPRoutes` - Filters disabled routes
2. `filterInvalidDestinationCallers` - Filters invalid destination callers
3. `filterLowTransfers` - Filters low-value transfers
4. **`filterNonWhitelistedDepositors`** - NEW: Filters non-whitelisted depositors

## Components

### 1. Configuration (`types/config.go`)

Added `DepositorWhitelistConfig` struct to the main `Config`:

```go
type DepositorWhitelistConfig struct {
    Enabled         bool   `yaml:"enabled"`
    QuickNodeAPIKey string `yaml:"quicknode-api-key"`
    QuickNodeKVKey  string `yaml:"quicknode-kv-key"`
    RefreshInterval uint   `yaml:"refresh-interval"` // seconds (default: 300)
}
```

**Default Values:**

- `refresh-interval`: 300 seconds (5 minutes) if omitted or set to 0

### 2. QuickNode KV Client (`types/quicknode_kv.go`)

HTTP client for interacting with QuickNode's KV store REST API:

- **Endpoint**: `GET https://api.quicknode.com/kv/rest/v1/lists/{key}`
- **Authentication**: `x-api-key` header
- **Response format**: `{"data": {"items": ["0x123...", "0x456..."]}}`
- **Timeout**: 10 seconds
- **Error handling**: Returns error for non-200 status codes

### 3. Whitelist Manager (`types/whitelist.go`)

Manages the in-memory whitelist cache with periodic refresh:

- **Thread-safe**: Uses `sync.RWMutex` for concurrent access
- **Normalization**: Converts addresses to lowercase for case-insensitive comparison
- **Validation**: Validates address format (42 characters, 0x prefix, hex characters)
- **Background refresh**: Goroutine that refreshes at configurable interval
- **Graceful failure**: Keeps existing cache if fetch fails
- **Methods**:
  - `Start(ctx)` - Starts background refresh goroutine
  - `IsWhitelisted(address)` - Checks if address is whitelisted
  - `Count()` - Returns number of whitelisted addresses

### 4. Depositor Extraction (`types/message_state.go`)

Added `GetDepositor()` method to `MessageState`:

```go
func (m *MessageState) GetDepositor() (string, error)
```

- Parses `MsgBody` as `BurnMessage`
- Extracts `MessageSender` (32 bytes)
- Takes last 20 bytes for Ethereum address
- Returns 0x-prefixed hex string

### 5. Filter Function (`cmd/process.go`)

New filter function `filterNonWhitelistedDepositors()`:

- **Scope**: Only applies to EVM chains (domains 0, 1, 2, 3)
- **Behavior**: Returns `true` if depositor should be filtered (not whitelisted)
- **Fail-safe**: Filters messages on depositor extraction errors
- **Logging**: Logs filtered transactions with depositor address
- **Metrics**: Increments Prometheus counter for filtered messages

### 6. Prometheus Metrics (`relayer/metrics.go`)

Added new counter metric:

- **Name**: `cctp_relayer_depositor_filtered_total`
- **Type**: Counter
- **Labels**: `source_domain`, `dest_domain`
- **Description**: Total number of messages filtered by depositor whitelist

## Configuration

### Config File (`config/sample-config.yaml`)

```yaml
depositor-whitelist:
  enabled: false # set to true to enable whitelist filtering
  quicknode-api-key: "" # QuickNode API key (can also be set via QUICKNODE_API_KEY env var)
  quicknode-kv-key: "cctp-depositor-whitelist" # Key name in QuickNode KV store
  refresh-interval: 300 # Refresh interval in seconds (default: 300 = 5 minutes if omitted or 0)
```

**Note:** If `refresh-interval` is omitted or set to 0, the default value of 300 seconds (5 minutes) will be used automatically to prevent crashes.

### Environment Variable

The QuickNode API key can be set via environment variable:

```bash
export QUICKNODE_API_KEY="your-api-key-here"
```

This takes precedence over the config file value.

## QuickNode KV Store Setup

### Data Structure

The whitelist should be stored in QuickNode's KV store as a list with the following structure:

```json
{
  "data": {
    "items": [
      "0x742d35Cc6634C0532925a3b844Bc9e7595f0bEb",
      "0x1234567890123456789012345678901234567890",
      "0xabcdefabcdefabcdefabcdefabcdefabcdefabcd"
    ]
  }
}
```

### Address Format

- Must be 42 characters (0x + 40 hex characters)
- Case-insensitive (normalized to lowercase internally)
- Example: `0x742d35Cc6634C0532925a3b844Bc9e7595f0bEb`

### API Access

The relayer will make GET requests to:

```
GET https://api.quicknode.com/kv/rest/v1/lists/{quicknode-kv-key}
Headers:
  - accept: application/json
  - Content-Type: application/json
  - x-api-key: {quicknode-api-key}
```

## Integration Points

### Startup (`cmd/process.go:Start()`)

1. After chains are initialized
2. Before processor workers start
3. Initializes `WhitelistManager` if enabled
4. Starts background refresh goroutine
5. Passes manager to all processor workers

### Processing Pipeline (`cmd/process.go:StartProcessor()`)

1. Message dequeued from processing queue
2. Filters applied in sequence (including new depositor filter)
3. If any filter returns `true`, message marked as `Filtered`
4. Filtered messages are not processed further

## EVM Domain Detection

The filter only applies to EVM source chains using the `isEVMDomain()` helper function:

**EVM Chains (Filtered):**

- **Domain 0**: Ethereum
- **Domain 1**: Avalanche
- **Domain 2**: OP Mainnet
- **Domain 3**: Arbitrum
- **Domain 6**: Base
- **Domain 7**: Polygon PoS
- **Domain 10**: Unichain
- **Domain 11**: Linea
- **Domain 12**: Codex
- **Domain 13**: Sonic
- **Domain 14**: World Chain
- **Domain 16**: Sei
- **Domain 17**: BNB Smart Chain
- **Domain 18**: XDC
- **Domain 19**: HyperEVM
- **Domain 21**: Ink
- **Domain 22**: Plume
- **Domain 26**: Arc Testnet

**Non-EVM Chains (Not Filtered):**

- **Domain 4**: Noble
- **Domain 5**: Solana
- **Domain 15**: Monad
- **Domain 25**: Starknet Testnet

## Error Handling

### QuickNode Fetch Failures

- Logs error and keeps existing cache
- Continues operating with last successful whitelist
- Retries on next refresh interval

### Depositor Extraction Failures

- Logs error with transaction hash
- Filters the message (fail-safe behavior)
- Prevents processing of malformed messages

### Empty Whitelist

- Logs info message
- Continues operating (filters all non-whitelisted depositors)

## Monitoring

### Logs

- **Info**: Whitelist enabled, refresh count, filtered transactions
- **Error**: API key missing, fetch failures, extraction failures
- **Debug**: Periodic refresh success

### Metrics

Query the depositor filter metric:

```promql
# Total filtered messages
cctp_relayer_depositor_filtered_total

# Filtered messages by source domain
cctp_relayer_depositor_filtered_total{source_domain="0"}

# Rate of filtered messages
rate(cctp_relayer_depositor_filtered_total[5m])
```

## Testing

### Manual Testing

1. Set up QuickNode KV store with test addresses
2. Enable whitelist in config
3. Send test transactions from whitelisted and non-whitelisted addresses
4. Verify filtered transactions in logs and metrics

### Verification

Check logs for:

```
Depositor whitelist enabled kv_key=cctp-depositor-whitelist refresh_interval=300
Initial whitelist loaded count=5
Filtered tx from non-whitelisted depositor tx=0x123... depositor=0xabc...
```

Check metrics endpoint:

```bash
curl http://localhost:2112/metrics | grep cctp_relayer_depositor_filtered_total
```

## Security Considerations

1. **API Key Protection**: Store QuickNode API key in environment variable, not config file
2. **Fail-Safe**: Malformed messages are filtered (rejected) rather than allowed
3. **Case-Insensitive**: Prevents bypass via case manipulation
4. **Validation**: Strict address format validation prevents injection

## Performance

- **Memory**: O(n) where n = number of whitelisted addresses
- **Lookup**: O(1) hash map lookup per message
- **Network**: One HTTP request per refresh interval (default: 5 minutes)
- **Concurrency**: Thread-safe with read-write locks

## Future Enhancements

Potential improvements for future versions:

1. Support for multiple KV stores or backends
2. Per-chain whitelist configuration
3. Whitelist caching to disk for faster startup
4. Metrics for whitelist size and refresh failures
5. Admin API for dynamic whitelist updates
