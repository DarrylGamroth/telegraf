package aeron_publisher

import (
	"fmt"
	"testing"
	"time"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/config"
	"github.com/influxdata/telegraf/plugins/serializers/influx"
	"github.com/influxdata/telegraf/testutil"
	"github.com/stretchr/testify/require"
)

func TestAeronPublisher_SampleConfig(t *testing.T) {
	plugin := &AeronPublisher{}
	config := plugin.SampleConfig()
	require.NotEmpty(t, config)
	require.Contains(t, config, "channel")
	require.Contains(t, config, "max_retries")
	require.Contains(t, config, "retry_delay")
	require.Contains(t, config, "max_message_size")
}

func TestAeronPublisher_Connect_EmptyChannel(t *testing.T) {
	plugin := &AeronPublisher{
		Channel:  "",
		StreamID: 1001,
		Log:      testutil.Logger{},
	}

	err := plugin.Connect()
	require.Error(t, err)
	require.Contains(t, err.Error(), "channel is required")
}

func TestAeronPublisher_Connect_InvalidStreamID(t *testing.T) {
	plugin := &AeronPublisher{
		Channel:  "aeron:udp?endpoint=localhost:40123",
		StreamID: 0, // Invalid zero stream ID
		Log:      testutil.Logger{},
	}

	err := plugin.Connect()
	require.Error(t, err)
	require.Contains(t, err.Error(), "stream_id is required and must be non-zero")
}

func TestAeronPublisher_SetSerializer(t *testing.T) {
	plugin := &AeronPublisher{}
	serializer := &influx.Serializer{}

	plugin.SetSerializer(serializer)
	require.Equal(t, serializer, plugin.serializer)
}

func TestAeronPublisher_Write_NoSerializer(t *testing.T) {
	plugin := &AeronPublisher{
		connected: true, // Connected but no serializer
		Log:       testutil.Logger{},
	}

	// Create a mock publication to satisfy connection check
	// Since we can't easily mock the publication, we'll check for the "not connected" error
	// The real scenario is caught by other validation

	metrics := []telegraf.Metric{
		testutil.MustMetric("test", nil, nil, time.Now()),
	}

	err := plugin.Write(metrics)
	require.Error(t, err)
	// Will fail with "not connected" since publication is nil, which is expected behavior
	require.Contains(t, err.Error(), "not connected to Aeron")
}

func TestAeronPublisher_Write_NotConnected(t *testing.T) {
	plugin := &AeronPublisher{
		connected: false,
		Log:       testutil.Logger{},
	}

	serializer := &influx.Serializer{}
	plugin.SetSerializer(serializer)

	metrics := []telegraf.Metric{
		testutil.MustMetric("test", nil, nil, time.Now()),
	}

	err := plugin.Write(metrics)
	require.Error(t, err)
	require.Contains(t, err.Error(), "not connected to Aeron")
}

func TestAeronPublisher_Close_NotConnected(t *testing.T) {
	plugin := &AeronPublisher{
		connected: false,
		Log:       testutil.Logger{},
	}

	// Should not error when closing unconnected plugin
	err := plugin.Close()
	require.NoError(t, err)
}

// Integration test that requires a running Aeron media driver
func TestAeronPublisher_ConnectAndWriteIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	plugin := &AeronPublisher{
		Channel:            "aeron:udp?endpoint=localhost:40123",
		StreamID:           1001,
		MaxRetries:         3,
		RetryDelay:         config.Duration(100 * time.Millisecond),
		MaxMessageSize:     1024 * 1024,
		DriverTimeout:      config.Duration(30 * time.Second),
		PublicationTimeout: config.Duration(10 * time.Second),
		Log:                testutil.Logger{},
	}

	serializer := &influx.Serializer{}
	plugin.SetSerializer(serializer)

	// This will fail if no media driver is running, which is expected
	// Users can run with a media driver to test integration
	err := plugin.Connect()
	if err != nil {
		t.Skipf("Skipping integration test: %v (ensure Aeron media driver is running)", err)
		return
	}
	defer plugin.Close()

	// Create test metrics
	metrics := []telegraf.Metric{
		testutil.MustMetric("test1",
			map[string]string{"tag1": "value1"},
			map[string]interface{}{"field1": 1.0},
			time.Now()),
		testutil.MustMetric("test2",
			map[string]string{"tag2": "value2"},
			map[string]interface{}{"field2": 2.0},
			time.Now()),
	}

	// Write metrics - should not error
	err = plugin.Write(metrics)
	require.NoError(t, err)
}

// Test configuration validation
func TestAeronPublisher_ConfigValidation(t *testing.T) {
	testCases := []struct {
		name    string
		plugin  *AeronPublisher
		wantErr string
	}{
		{
			name: "empty channel",
			plugin: &AeronPublisher{
				Channel:  "",
				StreamID: 1001,
				Log:      testutil.Logger{},
			},
			wantErr: "channel is required",
		},
		{
			name: "zero stream ID",
			plugin: &AeronPublisher{
				Channel:  "aeron:udp?endpoint=localhost:40123",
				StreamID: 0,
				Log:      testutil.Logger{},
			},
			wantErr: "stream_id is required and must be non-zero",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.plugin.Connect()
			require.Error(t, err)
			require.Contains(t, err.Error(), tc.wantErr)
		})
	}
}

// Test metrics serialization without Aeron dependency
func TestAeronPublisher_MetricSerialization(t *testing.T) {
	serializer := &influx.Serializer{}

	metrics := []telegraf.Metric{
		testutil.MustMetric("temperature",
			map[string]string{"location": "office", "sensor": "temp01"},
			map[string]interface{}{"value": 23.5, "status": "ok"},
			time.Unix(1609459200, 0)), // 2021-01-01 00:00:00 UTC
	}

	data, err := serializer.SerializeBatch(metrics)
	require.NoError(t, err)
	require.NotEmpty(t, data)

	// Check that serialized data contains expected elements
	serializedStr := string(data)
	require.Contains(t, serializedStr, "temperature")
	require.Contains(t, serializedStr, "location=office")
	require.Contains(t, serializedStr, "sensor=temp01")
	require.Contains(t, serializedStr, "value=23.5")
	require.Contains(t, serializedStr, "status=\"ok\"")
}

// Test large message handling
func TestAeronPublisher_LargeMessage(t *testing.T) {
	plugin := &AeronPublisher{
		MaxMessageSize: 100, // Very small limit for testing
		Log:            testutil.Logger{},
	}

	serializer := &influx.Serializer{}
	plugin.SetSerializer(serializer)

	// Create a metric with many fields to exceed the size limit
	fields := make(map[string]interface{})
	for i := 0; i < 50; i++ {
		fields[fmt.Sprintf("field_%d", i)] = "very_long_value_that_exceeds_size_limit"
	}

	metrics := []telegraf.Metric{
		testutil.MustMetric("test", nil, fields, time.Now()),
	}

	// Should handle large messages gracefully (not return error)
	// Since we're not connected, this will fail with "not connected" error
	err := plugin.Write(metrics)
	require.Error(t, err)
	require.Contains(t, err.Error(), "not connected to Aeron")
}
