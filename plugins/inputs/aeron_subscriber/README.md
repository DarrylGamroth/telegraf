# Aeron Subscriber Input Plugin

The Aeron subscriber input plugin subscribes to Aeron streams and converts incoming messages to Telegraf metrics using configurable data formats.

[Aeron](https://github.com/real-logic/aeron) is a low-latency, high-throughput messaging system designed for financial trading and other performance-critical applications. This plugin uses the [aeron-go](https://github.com/lirm/aeron-go) library to consume Aeron messages.

## Features

- Subscribe to UDP, IPC, or other Aeron transport channels
- Support for message fragmentation and reassembly via FragmentAssembler
- Configurable idle strategies for performance optimization
- Parser integration for various data formats (InfluxDB line protocol, JSON, CSV, etc.)
- Multiple subscription support via separate plugin instances
- Graceful shutdown and resource cleanup

## Global configuration options <!-- @/docs/includes/plugin_config.md -->

In addition to the plugin-specific configuration settings, plugins support additional global and plugin configuration settings. These settings are used to modify metrics, tags, and field or create aliases and configure ordering, etc. See the [CONFIGURATION.md][CONFIGURATION.md] for more details.

[CONFIGURATION.md]: ../../../docs/CONFIGURATION.md

## Configuration

```toml @sample.conf
# Aeron subscriber input plugin
[[inputs.aeron_subscriber]]
  ## Aeron directory (defaults to system temp + /aeron-<user>)
  ## Leave empty to use system default
  # aeron_dir = "/tmp/aeron-user"

  ## Channel to subscribe to
  ## Examples:
  ##   UDP: "aeron:udp?endpoint=localhost:40123"
  ##   IPC: "aeron:ipc"
  ##   UDP with interface: "aeron:udp?endpoint=localhost:40123|interface=localhost"
  channel = "aeron:udp?endpoint=localhost:40123"

  ## Stream ID to subscribe to
  stream_id = 10

  ## Media driver timeout for connection establishment
  # driver_timeout = "30s"

  ## Fragment limit per polling cycle
  ## Higher values can improve throughput but may increase latency
  # fragment_limit = 10

  ## Idle strategy for polling when no messages are available
  ## Options: "sleeping", "yielding", "busy", "backoff"
  # idle_strategy = "sleeping"

  ## Sleep duration when using "sleeping" idle strategy
  # idle_sleep_duration = "1ms"

  ## Data format for parsing incoming messages
  ## Each data format has its own unique set of configuration options, read
  ## more about them here:
  ## https://github.com/influxdata/telegraf/blob/master/docs/DATA_FORMATS_INPUT.md
  data_format = "influx"

## Multiple subscriptions example:
## Each [[inputs.aeron_subscriber]] block creates a separate plugin instance
# [[inputs.aeron_subscriber]]
#   channel = "aeron:udp?endpoint=localhost:40124"
#   stream_id = 11
#   data_format = "json"
#
# [[inputs.aeron_subscriber]]
#   channel = "aeron:ipc"
#   stream_id = 12
#   data_format = "csv"
```

## Configuration Options

### Required Parameters

- **`channel`** (string): The Aeron channel URI to subscribe to. Supports UDP and IPC transports.
- **`stream_id`** (int32): The stream ID to subscribe to on the specified channel.
- **`data_format`** (string): The data format for parsing incoming messages.

### Optional Parameters

- **`aeron_dir`** (string): Directory where Aeron driver files are stored. Defaults to system temp directory.
- **`driver_timeout`** (duration): Timeout for establishing connection to Aeron media driver. Default: `30s`.
- **`fragment_limit`** (int): Maximum number of fragments to process per polling cycle. Higher values improve throughput but may increase latency. Default: `10`.
- **`idle_strategy`** (string): Strategy to use when no messages are available. Options:
  - `"sleeping"`: Sleep for configured duration (lowest CPU usage)
  - `"yielding"`: Yield to other goroutines using `runtime.Gosched()`
  - `"busy"`: Busy wait (highest CPU usage, lowest latency)
  - `"backoff"`: Adaptive backoff strategy (balanced approach)
  Default: `"sleeping"`.
- **`idle_sleep_duration`** (duration): Sleep duration when using "sleeping" idle strategy. Default: `1ms`.

## Performance Tuning

### High Throughput Configuration
```toml
[[inputs.aeron_subscriber]]
  channel = "aeron:udp?endpoint=localhost:40123"
  stream_id = 10
  fragment_limit = 100      # Process more fragments per cycle
  idle_strategy = "busy"    # Lowest latency, highest CPU usage
  data_format = "influx"
```

### Low Resource Configuration
```toml
[[inputs.aeron_subscriber]]
  channel = "aeron:udp?endpoint=localhost:40123"
  stream_id = 10
  fragment_limit = 5        # Process fewer fragments per cycle
  idle_strategy = "sleeping"
  idle_sleep_duration = "5ms"  # Longer sleep for lower CPU usage
  data_format = "influx"
```

## Example Output

With InfluxDB line protocol data format:
```
cpu,host=server01 usage_idle=99.5,usage_user=0.3,usage_system=0.2 1609459200000000000
memory,host=server01 used_percent=45.2,available_bytes=8589934592 1609459200000000000
```

With JSON data format parsing trading data:
```
trade,symbol=EURUSD,side=buy price=1.2345,quantity=1000000,timestamp=1609459200000 1609459200000000000
trade,symbol=GBPUSD,side=sell price=1.3678,quantity=500000,timestamp=1609459200001 1609459200001000000
```

## Multiple Subscriptions

You can configure multiple Aeron subscriptions by defining multiple plugin instances:

```toml
# Trading data stream
[[inputs.aeron_subscriber]]
  channel = "aeron:udp?endpoint=192.168.1.100:40123"
  stream_id = 10
  data_format = "json"
  fragment_limit = 50
  
  [inputs.aeron_subscriber.tags]
    source = "trading"

# Market data stream  
[[inputs.aeron_subscriber]]
  channel = "aeron:udp?endpoint=192.168.1.100:40124"
  stream_id = 11
  data_format = "influx"
  fragment_limit = 100
  
  [inputs.aeron_subscriber.tags]
    source = "market_data"
```

## Error Handling

The plugin handles various error conditions gracefully:

- **Connection failures**: Plugin retries connection to Aeron media driver
- **Message parsing errors**: Invalid messages are logged and skipped
- **Network issues**: Temporary network problems are handled by Aeron's built-in reliability

## Requirements

- Aeron media driver must be running and accessible
- Network connectivity to specified endpoints (for UDP channels)
- Sufficient memory for message buffering
- Go 1.21+ (for building from source)

## Dependencies

This plugin uses the [aeron-go](https://github.com/lirm/aeron-go) library, which provides Go bindings for the Aeron messaging system.
