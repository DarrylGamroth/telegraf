# Aeron Media Driver Statistics Input Plugin

The Aeron Stat input plugin collects metrics from Aeron media driver Command and Control (CnC) files. This plugin provides detailed statistics about Aeron messaging performance, including message throughput, flow control events, and connection state.

## Global configuration options <!-- @/docs/includes/plugin_config.md -->

In addition to the plugin-specific configuration settings, plugins support additional global and plugin configuration settings. These settings are used to modify metrics, tags, and field or create aliases and configure ordering, etc. See the [CONFIGURATION.md][CONFIGURATION.md] for more details.

[CONFIGURATION.md]: ../../../docs/CONFIGURATION.md

## Configuration

```toml
[[inputs.aeron_stat]]
  ## Aeron directory path where CnC files are located
  ## Default: system default (usually /dev/shm/aeron or /tmp/aeron)
  # aeron_dir = "/dev/shm/aeron"
  
  ## Timeout for reading CnC files
  # read_timeout = "5s"
  
  ## Add custom tags to all metrics
  # [inputs.aeron_stat.tags]
  #   environment = "production"
  #   datacenter = "us-west-1"
```

## Metrics

The plugin generates several measurement types based on counter categories:

### aeron_bytes
Network throughput metrics.

**Tags:**
- `counter_id`: Unique counter identifier
- `type_id`: Counter type ID  
- `counter_type`: Human-readable counter type (e.g., "bytes_sent", "bytes_received")
- `channel`: Aeron channel (if present in label)
- `endpoint`: Network endpoint (if present in label)

**Fields:**
- `value` (integer): Counter value
- `label` (string): Original counter label
- `stream_id` (integer): Stream ID (if present in label)

### aeron_messages
Message-level metrics including NAKs, status messages, heartbeats.

**Tags:**
- `counter_id`: Unique counter identifier
- `type_id`: Counter type ID
- `counter_type`: Counter type ("nak_messages_sent", "status_messages_received", etc.)
- `channel`: Aeron channel (if present in label)
- `endpoint`: Network endpoint (if present in label)

**Fields:**
- `value` (integer): Message count
- `label` (string): Original counter label
- `stream_id` (integer): Stream ID (if present in label)

### aeron_flow_control  
Flow control and error metrics.

**Tags:**
- `counter_id`: Unique counter identifier
- `type_id`: Counter type ID
- `counter_type`: Counter type ("flow_control_under_runs", "errors", etc.)
- `channel`: Aeron channel (if present in label)

**Fields:**
- `value` (integer): Event count
- `label` (string): Original counter label

### aeron_positions
Position tracking metrics for receivers and senders.

**Tags:**
- `counter_id`: Unique counter identifier  
- `type_id`: Counter type ID
- `counter_type`: Counter type ("receiver_pos", "sender_pos", etc.)
- `channel`: Aeron channel (if present in label)

**Fields:**
- `value` (integer): Position value
- `label` (string): Original counter label

### aeron_streams
Publication and subscription metrics.

**Tags:**
- `counter_id`: Unique counter identifier
- `type_id`: Counter type ID  
- `counter_type`: Counter type ("publication_limit", "subscription_pos", etc.)
- `channel`: Aeron channel (if present in label)
- `endpoint`: Network endpoint (if present in label)

**Fields:**
- `value` (integer): Stream metric value
- `label` (string): Original counter label
- `stream_id` (integer): Stream ID (if present in label)

### aeron_clients
Client lifecycle metrics.

**Tags:**
- `counter_id`: Unique counter identifier
- `type_id`: Counter type ID
- `counter_type`: Counter type ("client_heartbeat", "client_keepalive")

**Fields:**
- `value` (integer): Client metric value  
- `label` (string): Original counter label

### aeron_system
System-level counters.

**Tags:**
- `counter_id`: Unique counter identifier
- `type_id`: Counter type ID
- `counter_type`: "system_counter"

**Fields:**
- `value` (integer): System counter value
- `label` (string): Original counter label

### aeron_counter
Fallback measurement for unknown counter types.

**Tags:**
- `counter_id`: Unique counter identifier
- `type_id`: Counter type ID
- `counter_type`: Counter type (e.g., "unknown_999")

**Fields:**
- `value` (integer): Counter value
- `label` (string): Original counter label

### aeron_stat_summary
Summary statistics about the collection process.

**Fields:**
- `total_counters` (integer): Total number of counters processed

## Counter Types

The plugin recognizes these Aeron counter types:

| Type ID | Counter Type | Description |
|---------|--------------|-------------|
| 0 | system_counter | System-level counters |
| 1 | bytes_sent | Bytes sent on publications |
| 2 | bytes_received | Bytes received on subscriptions |  
| 3 | failed_offers | Failed publication offers |
| 4 | nak_messages_sent | NAK messages sent |
| 5 | nak_messages_received | NAK messages received |
| 6 | status_messages_sent | Status messages sent |
| 7 | status_messages_received | Status messages received |
| 8 | heartbeats_sent | Heartbeat messages sent |
| 9 | heartbeats_received | Heartbeat messages received |
| 10 | retransmits_sent | Retransmit messages sent |
| 11 | flow_control_under_runs | Flow control under-runs |
| 12 | flow_control_over_runs | Flow control over-runs |
| 13 | invalid_packets | Invalid packets received |
| 14 | errors | Error count |
| 15 | short_sends | Short send operations |
| 21 | reception_rate | Message reception rate |
| 24 | receiver_pos | Receiver position |
| 25 | sender_pos | Sender position |
| 26 | sender_limit | Sender limit |
| 28 | publication_limit | Publication limit |
| 30 | subscription_pos | Subscription position |
| 32 | publisher_pos | Publisher position |
| 33 | publisher_limit | Publisher limit |
| 34 | client_heartbeat | Client heartbeat timestamp |
| 35 | client_keepalive | Client keepalive timestamp |

## Label Parsing

The plugin automatically parses counter labels to extract structured information:

- **Channel information**: Extracts `channel` and `endpoint` tags from Aeron URIs
- **Stream IDs**: Extracts `stream_id` field from labels
- **Key-value pairs**: Parses "key: value" patterns in labels

## Example Output

```
aeron_bytes,counter_id=1,type_id=1,counter_type=bytes_sent,channel=aeron:udp?endpoint=localhost:40123,endpoint=localhost:40123 value=1048576i,label="Publication: aeron:udp?endpoint=localhost:40123 stream-id=10",stream_id=10i 1609459200000000000

aeron_messages,counter_id=5,type_id=4,counter_type=nak_messages_sent value=42i,label="NAK messages sent" 1609459200000000000

aeron_stat_summary total_counters=25i 1609459200000000000
```

## Troubleshooting

### Media Driver Not Running
```
Error in plugin: failed to initialize CnC reader: failed to map CnC file
```
**Solution**: Ensure the Aeron media driver is running and the `aeron_dir` path is correct.

### Permission Issues  
```
Error in plugin: failed to map CnC file: permission denied
```
**Solution**: Ensure Telegraf has read permissions for the Aeron directory.

### Directory Not Found
```
Error in plugin: failed to map CnC file: no such file or directory  
```
**Solution**: Check that `aeron_dir` points to the correct location where the media driver creates CnC files.
