# Aeron Publisher Output Plugin

The Aeron Publisher output plugin publishes metrics to Aeron streams using the high-performance Aeron messaging system. This plugin leverages ExclusivePublication for optimal performance and supports various serialization formats.

## Features

- **High Performance**: Uses Aeron's ExclusivePublication for dedicated publishing
- **TryClaim Optimization**: Zero-copy direct writes for small messages
- **Backpressure Handling**: Configurable retry logic with exponential backoff
- **Multiple Formats**: Supports all Telegraf serialization formats (InfluxDB, JSON, CSV, etc.)
- **Monitoring**: Exposes internal metrics via Telegraf's selfstat system
- **Reliable**: Automatic reconnection and comprehensive error handling

## Configuration

```toml
[[outputs.aeron_publisher]]
  ## Aeron directory (defaults to system temp + /aeron-<user>)
  # aeron_dir = "/tmp/aeron-user"
  
  ## Channel to publish to
  channel = "aeron:udp?endpoint=localhost:40123"
  
  ## Stream ID for this publication
  stream_id = 10
  
  ## Media driver timeout for connection establishment
  # driver_timeout = "30s"
  
  ## Publication connection timeout
  # publication_timeout = "10s"
  
  ## Maximum message size for validation (optional - Aeron handles fragmentation automatically)
  ## Set this to validate metric size before publishing, or leave unset to allow any size
  # max_message_size = 65536
  
  ## Use TryClaim optimization for small messages (recommended for better performance)
  ## Messages smaller than MTU (~1400 bytes) can be written directly to term buffer
  # use_try_claim = true
  # try_claim_threshold = 1400
  
  ## Retry configuration for backpressure
  # max_retries = 3
  # retry_delay = "1ms"
  # retry_backoff_multiplier = 2.0
  # max_retry_delay = "100ms"
  
  ## Data format for serializing metrics
  data_format = "influx"
```

## Channel Types

Aeron supports multiple channel types:

- **UDP**: `aeron:udp?endpoint=host:port` - Network UDP transport
- **IPC**: `aeron:ipc` - Local inter-process communication
- **Custom**: Various custom transport options supported by Aeron

## Multiple Publications

You can configure multiple publications for different metric types or destinations:

```toml
[[outputs.aeron_publisher]]
  channel = "aeron:udp?endpoint=localhost:40123"
  stream_id = 10
  data_format = "influx"
  [outputs.aeron_publisher.tagpass]
    environment = ["production"]

[[outputs.aeron_publisher]]
  channel = "aeron:ipc"
  stream_id = 20
  data_format = "json"
  [outputs.aeron_publisher.tagpass]
    service = ["critical"]
```

## Exposed Metrics

The plugin exposes internal metrics for monitoring:

- `aeron_publisher_messages_sent` - Total messages successfully published
- `aeron_publisher_messages_dropped` - Messages dropped due to errors
- `aeron_publisher_bytes_transferred` - Total bytes transmitted
- `aeron_publisher_backpressure_errors` - Backpressure events
- `aeron_publisher_connection_errors` - Connection failures
- `aeron_publisher_retry_attempts` - Retry attempts made

Each metric includes `channel` and `stream_id` tags.

## Performance Tuning

- **TryClaim**: Enable for zero-copy writes of small messages
- **Backpressure**: Tune retry settings based on network conditions
- **Message Size**: Consider max_message_size for very large metrics
- **Serialization**: Choose appropriate format for your use case

## Prerequisites

Requires a running Aeron Media Driver. See [Aeron documentation](https://github.com/real-logic/aeron) for setup instructions.

## Status

**Development Phase**: This plugin is currently in development. Phase 1 (basic structure) is complete.

- ✅ Phase 1: Basic plugin structure and configuration
- 🚧 Phase 2: Aeron integration (in progress)
- ⏳ Phase 3: Metric serialization
- ⏳ Phase 4: Reliability features
- ⏳ Phase 5: Testing and documentation
