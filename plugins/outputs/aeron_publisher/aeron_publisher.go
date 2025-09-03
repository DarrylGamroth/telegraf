package aeron_publisher

import (
	_ "embed"
	"fmt"
	"sync"
	"time"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/config"
	"github.com/influxdata/telegraf/plugins/outputs"
	"github.com/influxdata/telegraf/selfstat"
	"github.com/lirm/aeron-go/aeron"
	"github.com/lirm/aeron-go/aeron/atomic"
)

//go:embed sample.conf
var sampleConfig string

// AeronPublisher implements the telegraf.Output interface for publishing metrics to Aeron streams
type AeronPublisher struct {
	AeronDir               string          `toml:"aeron_dir"`
	Channel                string          `toml:"channel"`
	StreamID               int32           `toml:"stream_id"`
	DriverTimeout          config.Duration `toml:"driver_timeout"`
	PublicationTimeout     config.Duration `toml:"publication_timeout"`
	MaxMessageSize         int             `toml:"max_message_size"`
	MaxRetries             int             `toml:"max_retries"`
	RetryDelay             config.Duration `toml:"retry_delay"`
	RetryBackoffMultiplier float64         `toml:"retry_backoff_multiplier"`
	MaxRetryDelay          config.Duration `toml:"max_retry_delay"`
	Log                    telegraf.Logger `toml:"-"`

	// Internal state
	serializer telegraf.Serializer
	connected  bool

	// Aeron objects
	aeronContext  *aeron.Context
	aeronInstance *aeron.Aeron
	publication   *aeron.Publication
	mutex         sync.RWMutex

	// Statistics (exposed as Telegraf metrics via selfstat)
	messagesSent       selfstat.Stat
	messagesDropped    selfstat.Stat
	bytesTransferred   selfstat.Stat
	backpressureErrors selfstat.Stat
	connectionErrors   selfstat.Stat
	retryAttempts      selfstat.Stat
}

// SampleConfig returns the sample configuration for the plugin
func (*AeronPublisher) SampleConfig() string {
	return sampleConfig
}

// SetSerializer sets the serializer for the plugin
func (a *AeronPublisher) SetSerializer(serializer telegraf.Serializer) {
	a.serializer = serializer
}

// Connect establishes connection to Aeron and creates the publication
func (a *AeronPublisher) Connect() error {
	// Set default values
	if a.DriverTimeout == 0 {
		a.DriverTimeout = config.Duration(30 * time.Second)
	}
	if a.PublicationTimeout == 0 {
		a.PublicationTimeout = config.Duration(10 * time.Second)
	}
	if a.MaxRetries == 0 {
		a.MaxRetries = 3
	}
	if a.RetryDelay == 0 {
		a.RetryDelay = config.Duration(1 * time.Millisecond)
	}
	if a.RetryBackoffMultiplier == 0 {
		a.RetryBackoffMultiplier = 2.0
	}
	if a.MaxRetryDelay == 0 {
		a.MaxRetryDelay = config.Duration(100 * time.Millisecond)
	}

	// Validate configuration
	if a.Channel == "" {
		return fmt.Errorf("channel is required")
	}
	if a.StreamID == 0 {
		return fmt.Errorf("stream_id is required and must be non-zero")
	}

	// Initialize selfstat metrics for monitoring plugin health
	tags := map[string]string{
		"channel":   a.Channel,
		"stream_id": fmt.Sprintf("%d", a.StreamID),
	}
	a.messagesSent = selfstat.Register("aeron_publisher", "messages_sent", tags)
	a.messagesDropped = selfstat.Register("aeron_publisher", "messages_dropped", tags)
	a.bytesTransferred = selfstat.Register("aeron_publisher", "bytes_transferred", tags)
	a.backpressureErrors = selfstat.Register("aeron_publisher", "backpressure_errors", tags)
	a.connectionErrors = selfstat.Register("aeron_publisher", "connection_errors", tags)
	a.retryAttempts = selfstat.Register("aeron_publisher", "retry_attempts", tags)

	a.Log.Infof("Connecting to Aeron: channel=%s, stream_id=%d", a.Channel, a.StreamID)

	// Create Aeron context
	a.aeronContext = aeron.NewContext()

	// Set Aeron directory if specified
	if a.AeronDir != "" {
		a.aeronContext.AeronDir(a.AeronDir)
	}

	// Set driver timeout
	a.aeronContext.MediaDriverTimeout(time.Duration(a.DriverTimeout))

	// Create Aeron instance
	aeronInstance, err := aeron.Connect(a.aeronContext)
	if err != nil {
		a.connectionErrors.Incr(1)
		return fmt.Errorf("failed to connect to Aeron: %w", err)
	}
	a.aeronInstance = aeronInstance

	// Add exclusive publication
	publication, err := a.aeronInstance.AddExclusivePublication(a.Channel, a.StreamID)
	if err != nil {
		a.connectionErrors.Incr(1)
		a.aeronInstance.Close()
		return fmt.Errorf("failed to add exclusive publication: %w", err)
	}

	// Wait for publication to be ready
	waitStart := time.Now()
	for time.Since(waitStart) < time.Duration(a.PublicationTimeout) {
		if publication.IsConnected() {
			a.publication = publication
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	if a.publication == nil {
		a.connectionErrors.Incr(1)
		a.aeronInstance.Close()
		return fmt.Errorf("publication not ready within timeout: %v", a.PublicationTimeout)
	}

	a.connected = true
	a.Log.Infof("Aeron publisher connected successfully")
	return nil
}

// Write publishes metrics to the Aeron stream
func (a *AeronPublisher) Write(metrics []telegraf.Metric) error {
	a.mutex.RLock()
	defer a.mutex.RUnlock()

	if !a.connected || a.publication == nil {
		return fmt.Errorf("not connected to Aeron")
	}

	if a.serializer == nil {
		return fmt.Errorf("serializer not set")
	}

	for _, metric := range metrics {
		data, err := a.serializer.Serialize(metric)
		if err != nil {
			a.Log.Errorf("Failed to serialize metric: %v", err)
			continue
		}

		// Optional size validation
		if a.MaxMessageSize > 0 && len(data) > a.MaxMessageSize {
			a.messagesDropped.Incr(1)
			a.Log.Warnf("Dropping metric: serialized size %d exceeds max_message_size %d",
				len(data), a.MaxMessageSize)
			continue
		}

		// Publish message with retry logic
		if err := a.publishMessage(data); err != nil {
			a.messagesDropped.Incr(1)
			a.Log.Errorf("Failed to publish metric: %v", err)
			continue
		}

		a.messagesSent.Incr(1)
		a.bytesTransferred.Incr(int64(len(data)))
	}

	return nil
}

// publishMessage handles the actual message publishing with retry logic
func (a *AeronPublisher) publishMessage(data []byte) error {
	var lastErr error
	delay := time.Duration(a.RetryDelay)

	for attempt := 0; attempt <= a.MaxRetries; attempt++ {
		if attempt > 0 {
			a.retryAttempts.Incr(1)
			time.Sleep(delay)
			delay = time.Duration(float64(delay) * a.RetryBackoffMultiplier)
			if delay > time.Duration(a.MaxRetryDelay) {
				delay = time.Duration(a.MaxRetryDelay)
			}
		}

		// Use standard Offer method
		result, err := a.offerMessage(data)
		if err != nil {
			lastErr = err
			continue
		}

		// Handle Aeron-specific result codes
		switch result {
		case aeron.BackPressured:
			a.backpressureErrors.Incr(1)
			lastErr = fmt.Errorf("backpressure: publication buffer full")
			continue
		case aeron.NotConnected:
			a.connectionErrors.Incr(1)
			lastErr = fmt.Errorf("publication not connected")
			continue
		case aeron.AdminAction:
			lastErr = fmt.Errorf("admin action required")
			continue
		case aeron.PublicationClosed:
			a.connectionErrors.Incr(1)
			lastErr = fmt.Errorf("publication closed")
			return lastErr // Don't retry on closed publication
		default:
			if result > 0 {
				// Success - result is the new stream position
				return nil
			}
			lastErr = fmt.Errorf("unknown result code: %d", result)
			continue
		}
	}

	return fmt.Errorf("failed after %d retries: %w", a.MaxRetries, lastErr)
}

// offerMessage uses the standard Offer method
func (a *AeronPublisher) offerMessage(data []byte) (int64, error) {
	// Create atomic buffer from data
	buffer := atomic.MakeBuffer(data)
	result := a.publication.Offer(buffer, 0, int32(len(data)), nil)
	return result, nil
}

// Close shuts down the Aeron connection and cleans up resources
func (a *AeronPublisher) Close() error {
	a.mutex.Lock()
	defer a.mutex.Unlock()

	if !a.connected {
		return nil
	}

	a.connected = false

	// Close publication
	if a.publication != nil {
		a.publication.Close()
		a.publication = nil
	}

	// Close Aeron instance
	if a.aeronInstance != nil {
		err := a.aeronInstance.Close()
		a.aeronInstance = nil
		if err != nil {
			a.Log.Errorf("Error closing Aeron instance: %v", err)
		}
	}

	// Clean up context
	a.aeronContext = nil

	a.Log.Infof("Aeron publisher closed")
	return nil
}

func init() {
	outputs.Add("aeron_publisher", func() telegraf.Output {
		return &AeronPublisher{}
	})
}
