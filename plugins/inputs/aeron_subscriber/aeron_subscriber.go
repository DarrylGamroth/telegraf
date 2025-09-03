//go:generate ../../../tools/readme_config_includer/generator
package aeron_subscriber

import (
	"context"
	_ "embed"
	"fmt"
	"sync"
	"time"

	"github.com/lirm/aeron-go/aeron"
	"github.com/lirm/aeron-go/aeron/atomic"
	"github.com/lirm/aeron-go/aeron/idlestrategy"
	"github.com/lirm/aeron-go/aeron/logbuffer"
	"github.com/lirm/aeron-go/aeron/logbuffer/term"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/config"
	"github.com/influxdata/telegraf/plugins/inputs"
)

//go:embed sample.conf
var sampleConfig string

// AeronSubscriber represents the Aeron subscriber input plugin
type AeronSubscriber struct {
	// Configuration options
	AeronDir          string          `toml:"aeron_dir"`
	Channel           string          `toml:"channel"`
	StreamID          int32           `toml:"stream_id"`
	DriverTimeout     config.Duration `toml:"driver_timeout"`
	FragmentLimit     int             `toml:"fragment_limit"`
	IdleStrategy      string          `toml:"idle_strategy"`
	IdleSleepDuration config.Duration `toml:"idle_sleep_duration"`
	Log               telegraf.Logger `toml:"-"`

	// Internal state
	parser             telegraf.Parser
	aeron              *aeron.Aeron
	subscription       *aeron.Subscription
	assembler          *aeron.FragmentAssembler
	currentAccumulator telegraf.Accumulator // Store current accumulator for fragment handler
	cancel             context.CancelFunc
	wg                 *sync.WaitGroup
}

// SampleConfig returns the sample configuration for the plugin
func (*AeronSubscriber) SampleConfig() string {
	return sampleConfig
}

// Init initializes the plugin and validates configuration
func (a *AeronSubscriber) Init() error {
	// Set defaults
	if a.DriverTimeout == 0 {
		a.DriverTimeout = config.Duration(30 * time.Second)
	}

	if a.FragmentLimit == 0 {
		a.FragmentLimit = 10
	}

	if a.IdleStrategy == "" {
		a.IdleStrategy = "backoff"
	}

	if a.IdleSleepDuration == 0 {
		a.IdleSleepDuration = config.Duration(1 * time.Millisecond)
	}

	// Validate required configuration
	if a.Channel == "" {
		return fmt.Errorf("channel is required")
	}

	if a.StreamID < 0 {
		return fmt.Errorf("stream_id must be non-negative")
	}

	return nil
}

// SetParser sets the parser for incoming messages
func (a *AeronSubscriber) SetParser(parser telegraf.Parser) {
	a.parser = parser
}

// Start begins consuming messages from the Aeron stream
func (a *AeronSubscriber) Start(acc telegraf.Accumulator) error {
	a.Log.Info("Starting Aeron subscriber plugin")

	// Setup Aeron connection
	if err := a.connect(); err != nil {
		return fmt.Errorf("failed to connect to Aeron: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	a.cancel = cancel

	a.wg = &sync.WaitGroup{}
	a.wg.Add(1)
	go func() {
		defer a.wg.Done()
		a.consume(ctx, acc)
	}()

	return nil
}

// connect establishes the Aeron connection and subscription
func (a *AeronSubscriber) connect() error {
	a.Log.Debugf("Connecting to Aeron with channel=%s, streamID=%d", a.Channel, a.StreamID)

	// Create Aeron context with configuration
	ctx := aeron.NewContext()

	// Set aeron directory if specified
	if a.AeronDir != "" {
		ctx.AeronDir(a.AeronDir)
	}

	// Set media driver timeout
	ctx.MediaDriverTimeout(time.Duration(a.DriverTimeout))

	// Connect to Aeron
	var err error
	a.aeron, err = aeron.Connect(ctx)
	if err != nil {
		return fmt.Errorf("failed to connect to Aeron: %w", err)
	}

	// Add subscription
	a.subscription, err = a.aeron.AddSubscription(a.Channel, a.StreamID)
	if err != nil {
		a.aeron.Close()
		return fmt.Errorf("failed to add subscription: %w", err)
	}

	// Create fragment assembler with fragment handler
	a.assembler = aeron.NewFragmentAssembler(a.createFragmentHandler(), aeron.DefaultFragmentAssemblyBufferLength)

	a.Log.Infof("Successfully connected to Aeron channel=%s, streamID=%d", a.Channel, a.StreamID)
	return nil
}

// createFragmentHandler creates a fragment handler that processes incoming messages
func (a *AeronSubscriber) createFragmentHandler() term.FragmentHandler {
	return func(buffer *atomic.Buffer, offset int32, length int32, header *logbuffer.Header) {
		// Extract message data
		data := buffer.GetBytesArray(offset, length)

		a.Log.Debugf("Received fragment: offset=%d, length=%d, sessionId=%d",
			offset, length, header.SessionId())

		// Parse with configured parser
		metrics, err := a.parser.Parse(data)
		if err != nil {
			a.Log.Errorf("Failed to parse message: %v", err)
			return
		}

		// Add metrics to accumulator (stored in context via closure)
		if acc := a.getAccumulator(); acc != nil {
			for _, metric := range metrics {
				acc.AddMetric(metric)
			}
			a.Log.Debugf("Added %d metrics to accumulator", len(metrics))
		}
	}
}

// getAccumulator gets the current accumulator from the context
// This is a simple implementation using a stored reference
func (a *AeronSubscriber) getAccumulator() telegraf.Accumulator {
	// For now, we'll store the accumulator in the consume goroutine
	// In a production implementation, we might use a channel or other sync mechanism
	return a.currentAccumulator
}

// createIdleStrategy creates an idle strategy based on configuration
func (a *AeronSubscriber) createIdleStrategy() (idlestrategy.Idler, error) {
	switch a.IdleStrategy {
	case "sleeping":
		duration := time.Duration(a.IdleSleepDuration)
		return &idlestrategy.Sleeping{SleepFor: duration}, nil
	case "yielding":
		return &idlestrategy.Yielding{}, nil
	case "busy":
		return &idlestrategy.Busy{}, nil
	case "backoff":
		return idlestrategy.NewDefaultBackoffIdleStrategy(), nil
	default:
		return nil, fmt.Errorf("unknown idle strategy: %s", a.IdleStrategy)
	}
}

// consume handles the main message consumption loop
func (a *AeronSubscriber) consume(ctx context.Context, acc telegraf.Accumulator) {
	a.Log.Info("Starting message consumption goroutine")

	// Create idle strategy based on configuration
	idleStrategy, err := a.createIdleStrategy()
	if err != nil {
		a.Log.Errorf("Failed to create idle strategy: %v", err)
		return
	}

	// Main polling loop
	for {
		select {
		case <-ctx.Done():
			a.Log.Info("Context cancelled, stopping consumption")
			return
		default:
			// Poll for fragments with configured limit
			fragmentsRead := a.subscription.Poll(a.assembler.OnFragment, a.FragmentLimit)

			// Use idle strategy - it will internally decide whether to idle based on fragmentsRead
			idleStrategy.Idle(fragmentsRead)

			if fragmentsRead > 0 {
				a.Log.Debugf("Read %d fragments", fragmentsRead)
			}
		}
	}
}

// Gather is called by Telegraf to collect metrics
// For streaming input plugins, this typically returns nil
func (a *AeronSubscriber) Gather(acc telegraf.Accumulator) error {
	return nil
}

// Stop shuts down the plugin and cleans up resources
func (a *AeronSubscriber) Stop() {
	a.Log.Info("Stopping Aeron subscriber plugin")

	if a.cancel != nil {
		a.cancel()
	}

	if a.wg != nil {
		a.wg.Wait()
	}

	// Phase 2: Aeron connection cleanup will be added here
	a.Log.Info("Aeron subscriber plugin stopped")
}

// init registers the plugin with Telegraf
func init() {
	inputs.Add("aeron_subscriber", func() telegraf.Input {
		return &AeronSubscriber{}
	})
}
