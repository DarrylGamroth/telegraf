package aeron_stat

import (
	"testing"
	"time"

	"github.com/influxdata/telegraf/config"
	"github.com/influxdata/telegraf/testutil"
	"github.com/stretchr/testify/require"
)

func TestAeronStat_Init_Defaults(t *testing.T) {
	plugin := &AeronStat{}
	plugin.Log = testutil.Logger{}

	err := plugin.Init()
	require.NoError(t, err)

	// Check default values
	require.Equal(t, config.Duration(5*time.Second), plugin.ReadTimeout)
	require.NotEmpty(t, plugin.AeronDir)
	require.Contains(t, plugin.AeronDir, "aeron-")
}

func TestAeronStat_Init_CustomConfig(t *testing.T) {
	plugin := &AeronStat{
		AeronDir:    "/custom/aeron/dir",
		ReadTimeout: config.Duration(10 * time.Second),
		Tags:        map[string]string{"env": "test"},
	}
	plugin.Log = testutil.Logger{}

	err := plugin.Init()
	require.NoError(t, err)

	// Check configured values
	require.Equal(t, "/custom/aeron/dir", plugin.AeronDir)
	require.Equal(t, config.Duration(10*time.Second), plugin.ReadTimeout)
	require.Equal(t, "test", plugin.Tags["env"])
}

func TestAeronStat_Description(t *testing.T) {
	plugin := &AeronStat{}
	require.NotEmpty(t, plugin.Description())
}

func TestAeronStat_SampleConfig(t *testing.T) {
	plugin := &AeronStat{}
	config := plugin.SampleConfig()
	require.NotEmpty(t, config)
	require.Contains(t, config, "aeron_dir")
	require.Contains(t, config, "read_timeout")
}

func TestAeronStat_GatherWithoutDriver(t *testing.T) {
	// Skip this test in short mode since it requires external dependencies
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	plugin := &AeronStat{
		AeronDir: "/nonexistent/path",
		Log:      testutil.Logger{},
	}

	err := plugin.Init()
	require.NoError(t, err)

	acc := &testutil.Accumulator{}
	err = plugin.Gather(acc)
	// Should fail gracefully when driver is not available
	require.Error(t, err)
	require.Contains(t, err.Error(), "failed to map CnC file")
}

func TestAeronStat_Cleanup(t *testing.T) {
	plugin := &AeronStat{}
	plugin.Log = testutil.Logger{}

	// cleanup should not panic even when called with no initialized resources
	plugin.cleanup()
	plugin.Stop()

	require.Nil(t, plugin.reader)
	require.Nil(t, plugin.counterFile)
	require.Nil(t, plugin.cncFile)
}

func TestAeronStat_ParseCounterType(t *testing.T) {
	plugin := &AeronStat{}

	testCases := []struct {
		typeId   int32
		expected string
	}{
		{0, "system_counter"},
		{1, "bytes_sent"},
		{2, "bytes_received"},
		{4, "nak_messages_sent"},
		{14, "errors"},
		{999, "unknown_999"},
	}

	for _, tc := range testCases {
		result := plugin.parseCounterType(tc.typeId)
		require.Equal(t, tc.expected, result, "Type ID %d should return %s", tc.typeId, tc.expected)
	}
}

func TestAeronStat_ParseCounterLabel(t *testing.T) {
	plugin := &AeronStat{}

	testCases := []struct {
		label       string
		expectTags  map[string]string
		expectField string
	}{
		{
			label:       "Simple label",
			expectTags:  map[string]string{},
			expectField: "Simple label",
		},
		{
			label:       "aeron:udp?endpoint=localhost:40123 stream-id=10",
			expectTags:  map[string]string{"channel": "aeron:udp?endpoint=localhost:40123", "endpoint": "localhost:40123"},
			expectField: "aeron:udp?endpoint=localhost:40123 stream-id=10",
		},
		{
			label:       "Publication: aeron:udp?endpoint=localhost:40123 stream-id=10",
			expectTags:  map[string]string{"channel": "aeron:udp?endpoint=localhost:40123", "endpoint": "localhost:40123"},
			expectField: "Publication: aeron:udp?endpoint=localhost:40123 stream-id=10",
		},
	}

	for _, tc := range testCases {
		result := plugin.parseCounterLabel(tc.label)
		require.Equal(t, tc.expectField, result.fields["label"], "Label should be preserved")

		for key, expectedValue := range tc.expectTags {
			actualValue, exists := result.tags[key]
			require.True(t, exists, "Expected tag %s to exist", key)
			require.Equal(t, expectedValue, actualValue, "Tag %s should have value %s", key, expectedValue)
		}
	}
}

func TestAeronStat_GetMeasurementName(t *testing.T) {
	plugin := &AeronStat{}

	testCases := []struct {
		typeId      int32
		counterType string
		expected    string
	}{
		{1, "bytes_sent", "aeron_bytes"},
		{2, "bytes_received", "aeron_bytes"},
		{4, "nak_messages_sent", "aeron_messages"},
		{14, "errors", "aeron_flow_control"},
		{21, "reception_rate", "aeron_positions"},
		{30, "subscription_pos", "aeron_streams"},
		{34, "client_heartbeat", "aeron_clients"},
		{0, "system_counter", "aeron_system"},
		{999, "unknown_999", "aeron_counter"},
	}

	for _, tc := range testCases {
		result := plugin.getMeasurementName(tc.typeId, tc.counterType)
		require.Equal(t, tc.expected, result, "Type ID %d should use measurement %s", tc.typeId, tc.expected)
	}
}
