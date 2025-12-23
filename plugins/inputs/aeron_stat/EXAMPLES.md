# Aeron Stat Plugin Examples

This document provides practical examples of using the Aeron Stat input plugin.

## Basic Configuration

### Minimal Setup
```toml
[[inputs.aeron_stat]]
  # Uses default aeron directory and 5s read timeout
```

### Custom Directory
```toml
[[inputs.aeron_stat]]
  aeron_dir = "/dev/shm/my-aeron"
  read_timeout = "10s"
```

### With Custom Tags
```toml
[[inputs.aeron_stat]]
  aeron_dir = "/opt/aeron/data"
  read_timeout = "2s"
  
  [inputs.aeron_stat.tags]
    environment = "production"
    datacenter = "us-west-1"
    service = "messaging"
```

## Complete Telegraf Configuration

### Basic Monitoring Setup
```toml
[agent]
  interval = "10s"
  flush_interval = "10s"

[[inputs.aeron_stat]]
  aeron_dir = "/dev/shm/aeron"
  read_timeout = "5s"
  
  [inputs.aeron_stat.tags]
    environment = "production"

[[outputs.influxdb_v2]]
  urls = ["http://localhost:8086"]
  token = "your-token"
  organization = "your-org"
  bucket = "aeron-metrics"
```

### With Filtering and Aggregation
```toml
[agent]
  interval = "5s"
  flush_interval = "10s"

[[inputs.aeron_stat]]
  aeron_dir = "/dev/shm/aeron"
  
  [inputs.aeron_stat.tags]
    host = "aeron-node-1"

# Only collect error-related metrics
[[processors.filter]]
  namepass = ["aeron_flow_control", "aeron_messages"]
  [processors.filter.tagpass]
    counter_type = ["errors", "nak_*", "flow_control_*"]

# Aggregate by measurement type
[[aggregators.basicstats]]
  period = "30s"
  drop_original = false
  stats = ["count", "sum", "mean"]
  namepass = ["aeron_bytes", "aeron_messages"]

[[outputs.file]]
  files = ["stdout"]
  data_format = "influx"
```

## Use Cases

### High-Frequency Trading Monitor
```toml
[agent]
  interval = "1s"
  flush_interval = "1s"

[[inputs.aeron_stat]]
  aeron_dir = "/dev/shm/aeron-hft"
  read_timeout = "500ms"
  
  [inputs.aeron_stat.tags]
    trading_venue = "NYSE"
    strategy = "market_making"

# Focus on latency-critical metrics
[[processors.filter]]
  namepass = ["aeron_bytes", "aeron_positions", "aeron_flow_control"]

[[outputs.influxdb_v2]]
  urls = ["http://timeseries-db:8086"]
  token = "trading-metrics-token"
  organization = "trading-firm"
  bucket = "hft-metrics"
```

### Multi-Node Cluster Monitoring
```toml
[agent]
  interval = "10s"
  flush_interval = "30s"

[[inputs.aeron_stat]]
  aeron_dir = "/opt/aeron/node1"
  
  [inputs.aeron_stat.tags]
    cluster_node = "node-1"
    cluster_role = "leader"

[[inputs.aeron_stat]]
  aeron_dir = "/opt/aeron/node2"
  
  [inputs.aeron_stat.tags]
    cluster_node = "node-2" 
    cluster_role = "follower"

[[outputs.prometheus_client]]
  listen = ":9273"
  metric_version = 2
```

### Development Environment
```toml
[agent]
  interval = "30s"
  flush_interval = "30s"
  debug = true

[[inputs.aeron_stat]]
  aeron_dir = "/tmp/aeron-dev"
  read_timeout = "10s"
  
  [inputs.aeron_stat.tags]
    environment = "development"
    developer = "alice"

# Log all metrics to file for debugging
[[outputs.file]]
  files = ["/tmp/aeron-debug.log"]
  data_format = "json"
  json_timestamp_format = "2006-01-02T15:04:05.000Z"
```

## Sample Queries

### InfluxDB Queries

**Total bytes transferred:**
```sql
SELECT sum("value") 
FROM "aeron_bytes" 
WHERE time >= now() - 1h 
GROUP BY "counter_type"
```

**Error rate over time:**
```sql
SELECT mean("value") 
FROM "aeron_flow_control" 
WHERE "counter_type" = 'errors' 
AND time >= now() - 1h 
GROUP BY time(1m)
```

**Stream throughput:**
```sql
SELECT derivative(mean("value"), 1s) 
FROM "aeron_bytes" 
WHERE "counter_type" = 'bytes_sent' 
AND time >= now() - 5m 
GROUP BY "stream_id", time(10s)
```

### Prometheus Queries

**Message rate:**
```promql
rate(aeron_messages_value[5m])
```

**Error ratio:**
```promql
rate(aeron_flow_control_value{counter_type="errors"}[5m]) / 
rate(aeron_messages_value[5m])
```

**Top streams by throughput:**
```promql
topk(5, rate(aeron_bytes_value{counter_type="bytes_sent"}[1m]))
```

## Troubleshooting Examples

### Debugging Connection Issues
```toml
[agent]
  debug = true
  quiet = false

[[inputs.aeron_stat]]
  aeron_dir = "/dev/shm/aeron"
  read_timeout = "30s"

[[outputs.file]]
  files = ["stdout"]
  data_format = "influx"
```

### Monitoring Plugin Health
```toml
[[inputs.aeron_stat]]
  aeron_dir = "/dev/shm/aeron"

# Monitor the summary metrics to ensure plugin is working
[[processors.filter]]
  namepass = ["aeron_stat_summary"]

[[outputs.influxdb_v2]]
  urls = ["http://localhost:8086"]
  # ... other config
```

Run Telegraf with debug logging:
```bash
./telegraf --config aeron-debug.conf --debug
```
