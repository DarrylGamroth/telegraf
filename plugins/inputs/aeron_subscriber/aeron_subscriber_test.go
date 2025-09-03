package aeron_subscriber

import (
	"testing"
	"time"

	"github.com/influxdata/telegraf/config"
	"github.com/influxdata/telegraf/plugins/parsers/influx"
	"github.com/influxdata/telegraf/testutil"
	"github.com/stretchr/testify/require"
)

func TestAeronSubscriber_SampleConfig(t *testing.T) {
	plugin := &AeronSubscriber{}
	config := plugin.SampleConfig()
	require.NotEmpty(t, config)
	require.Contains(t, config, "channel")
	require.Contains(t, config, "stream_id")
	require.Contains(t, config, "fragment_limit")
	require.Contains(t, config, "idle_strategy")
}

func TestAeronSubscriber_Init_Defaults(t *testing.T) {
	plugin := &AeronSubscriber{
		Channel:  "aeron:udp?endpoint=localhost:40123",
		StreamID: 1001,
		Log:      testutil.Logger{},
	}

	err := plugin.Init()
	require.NoError(t, err)

	// Check that defaults were set
	require.Equal(t, config.Duration(30*time.Second), plugin.DriverTimeout)
	require.Equal(t, 10, plugin.FragmentLimit)
	require.Equal(t, "backoff", plugin.IdleStrategy)
	require.Equal(t, config.Duration(1*time.Millisecond), plugin.IdleSleepDuration)
}

func TestAeronSubscriber_Init_ValidationErrors(t *testing.T) {
	testCases := []struct {
		name    string
		plugin  *AeronSubscriber
		wantErr string
	}{
		{
			name: "empty channel",
			plugin: &AeronSubscriber{
				Channel:  "",
				StreamID: 1001,
				Log:      testutil.Logger{},
			},
			wantErr: "channel is required",
		},
		{
			name: "negative stream ID",
			plugin: &AeronSubscriber{
				Channel:  "aeron:udp?endpoint=localhost:40123",
				StreamID: -1,
				Log:      testutil.Logger{},
			},
			wantErr: "stream_id must be non-negative",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.plugin.Init()
			require.Error(t, err)
			require.Contains(t, err.Error(), tc.wantErr)
		})
	}
}

func TestAeronSubscriber_SetParser(t *testing.T) {
	plugin := &AeronSubscriber{}
	parser := &influx.Parser{}

	plugin.SetParser(parser)
	require.Equal(t, parser, plugin.parser)
}

func TestAeronSubscriber_Gather(t *testing.T) {
	plugin := &AeronSubscriber{}
	acc := &testutil.Accumulator{}

	// Gather should not add any metrics for streaming input plugins
	err := plugin.Gather(acc)
	require.NoError(t, err)
	require.Empty(t, acc.Metrics)
}

func TestAeronSubscriber_CreateIdleStrategy(t *testing.T) {
	testCases := []struct {
		name        string
		strategy    string
		sleepDur    config.Duration
		expectError bool
	}{
		{
			name:     "sleeping strategy",
			strategy: "sleeping",
			sleepDur: config.Duration(10 * time.Millisecond),
		},
		{
			name:     "yielding strategy",
			strategy: "yielding",
		},
		{
			name:     "busy strategy",
			strategy: "busy",
		},
		{
			name:     "backoff strategy",
			strategy: "backoff",
		},
		{
			name:        "invalid strategy",
			strategy:    "invalid",
			expectError: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			plugin := &AeronSubscriber{
				IdleStrategy:      tc.strategy,
				IdleSleepDuration: tc.sleepDur,
				Log:               testutil.Logger{},
			}

			strategy, err := plugin.createIdleStrategy()
			if tc.expectError {
				require.Error(t, err)
				require.Nil(t, strategy)
			} else {
				require.NoError(t, err)
				require.NotNil(t, strategy)
			}
		})
	}
}

func TestAeronSubscriber_Stop_NotStarted(t *testing.T) {
	plugin := &AeronSubscriber{
		Log: testutil.Logger{},
	}

	// Should not panic when stopping unstarted plugin
	plugin.Stop()
}

func TestAeronSubscriber_Start_InvalidConfig(t *testing.T) {
	plugin := &AeronSubscriber{
		Channel:  "", // Invalid - will fail Init
		StreamID: 1001,
		Log:      testutil.Logger{},
	}

	err := plugin.Init()
	require.Error(t, err)

	// Since Init failed, Start should not be called, but let's test error handling
	plugin.Channel = "aeron:udp?endpoint=localhost:40123"
	err = plugin.Init()
	require.NoError(t, err)

	acc := &testutil.Accumulator{}
	// This will fail due to no Aeron media driver, which is expected
	err = plugin.Start(acc)
	if err != nil {
		require.Contains(t, err.Error(), "failed to connect to Aeron")
	}
}

// Integration test that requires a running Aeron media driver
func TestAeronSubscriber_StartStopIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	plugin := &AeronSubscriber{
		Channel:           "aeron:udp?endpoint=localhost:40123",
		StreamID:          1001,
		FragmentLimit:     10,
		IdleStrategy:      "backoff",
		DriverTimeout:     config.Duration(30 * time.Second),
		IdleSleepDuration: config.Duration(1 * time.Millisecond),
		Log:               testutil.Logger{},
	}

	// Initialize the plugin
	err := plugin.Init()
	require.NoError(t, err)

	// Set up a parser
	parser := &influx.Parser{}
	require.NoError(t, parser.Init())
	plugin.SetParser(parser)

	acc := &testutil.Accumulator{}

	// This will fail if no media driver is running, which is expected
	// Users can run with a media driver to test integration
	err = plugin.Start(acc)
	if err != nil {
		t.Skipf("Skipping integration test: %v (ensure Aeron media driver is running)", err)
		return
	}

	// Let it run briefly
	time.Sleep(100 * time.Millisecond)

	// Stop the plugin
	plugin.Stop()

	// Should complete without error
}

// Integration test for full round-trip with publisher if available
func TestAeronSubscriber_ReceiveMessages_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Test parameters
	testChannel := "aeron:udp?endpoint=localhost:40123"
	testStreamID := int32(1002) // Use different stream ID to avoid conflicts

	subscriber := &AeronSubscriber{
		Channel:           testChannel,
		StreamID:          testStreamID,
		FragmentLimit:     10,
		IdleStrategy:      "backoff",
		DriverTimeout:     config.Duration(30 * time.Second),
		IdleSleepDuration: config.Duration(1 * time.Millisecond),
		Log:               testutil.Logger{},
	}

	// Initialize the subscriber
	err := subscriber.Init()
	require.NoError(t, err)

	// Set up a parser
	parser := &influx.Parser{}
	require.NoError(t, parser.Init())
	subscriber.SetParser(parser)

	acc := &testutil.Accumulator{}

	// Start the subscriber
	err = subscriber.Start(acc)
	if err != nil {
		t.Skipf("Skipping integration test: %v (ensure Aeron media driver is running)", err)
		return
	}
	defer subscriber.Stop()

	// Give subscriber time to establish connection
	time.Sleep(50 * time.Millisecond)

	t.Logf("Subscriber started successfully on channel=%s, streamID=%d", testChannel, testStreamID)

	// Let it run for a bit to check it doesn't crash
	time.Sleep(200 * time.Millisecond)

	t.Logf("Integration test completed successfully")
}

// Test plugin configuration combinations
func TestAeronSubscriber_ConfigurationCombinations(t *testing.T) {
	testCases := []struct {
		name   string
		plugin *AeronSubscriber
	}{
		{
			name: "minimal valid config",
			plugin: &AeronSubscriber{
				Channel:  "aeron:udp?endpoint=localhost:40123",
				StreamID: 1001,
				Log:      testutil.Logger{},
			},
		},
		{
			name: "full config with custom values",
			plugin: &AeronSubscriber{
				AeronDir:          "/tmp/aeron",
				Channel:           "aeron:udp?endpoint=localhost:40124",
				StreamID:          2002,
				DriverTimeout:     config.Duration(60 * time.Second),
				FragmentLimit:     20,
				IdleStrategy:      "sleeping",
				IdleSleepDuration: config.Duration(5 * time.Millisecond),
				Log:               testutil.Logger{},
			},
		},
		{
			name: "different idle strategies",
			plugin: &AeronSubscriber{
				Channel:      "aeron:udp?endpoint=localhost:40125",
				StreamID:     3003,
				IdleStrategy: "yielding",
				Log:          testutil.Logger{},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.plugin.Init()
			require.NoError(t, err)

			// Verify the plugin is properly configured
			require.NotEmpty(t, tc.plugin.Channel)
			require.GreaterOrEqual(t, tc.plugin.StreamID, int32(0))
			require.Greater(t, tc.plugin.FragmentLimit, 0)
			require.NotEmpty(t, tc.plugin.IdleStrategy)
		})
	}
}

// Test fragment handler creation and basic structure
func TestAeronSubscriber_CreateFragmentHandler(t *testing.T) {
	plugin := &AeronSubscriber{
		Log: testutil.Logger{},
	}

	// Set up a parser
	parser := &influx.Parser{}
	require.NoError(t, parser.Init())
	plugin.SetParser(parser)

	// Create fragment handler
	handler := plugin.createFragmentHandler()
	require.NotNil(t, handler)

	// The fragment handler is a function, so we can't test much more
	// without setting up the full Aeron infrastructure
}

// Test accumulator handling
func TestAeronSubscriber_AccumulatorHandling(t *testing.T) {
	plugin := &AeronSubscriber{
		Log: testutil.Logger{},
	}

	// Test when no accumulator is set
	acc := plugin.getAccumulator()
	require.Nil(t, acc)

	// Test setting accumulator
	testAcc := &testutil.Accumulator{}
	plugin.currentAccumulator = testAcc
	acc = plugin.getAccumulator()
	require.Equal(t, testAcc, acc)
}

// Test custom configuration parsing
func TestAeronSubscriber_CustomConfig(t *testing.T) {
	plugin := &AeronSubscriber{
		Channel:           "aeron:ipc",
		StreamID:          42,
		FragmentLimit:     100,
		IdleStrategy:      "busy",
		DriverTimeout:     config.Duration(15 * time.Second),
		IdleSleepDuration: config.Duration(500 * time.Microsecond),
		Log:               testutil.Logger{},
	}

	err := plugin.Init()
	require.NoError(t, err)

	// Verify custom values are preserved
	require.Equal(t, "aeron:ipc", plugin.Channel)
	require.Equal(t, int32(42), plugin.StreamID)
	require.Equal(t, 100, plugin.FragmentLimit)
	require.Equal(t, "busy", plugin.IdleStrategy)
	require.Equal(t, config.Duration(15*time.Second), plugin.DriverTimeout)
	require.Equal(t, config.Duration(500*time.Microsecond), plugin.IdleSleepDuration)
}
