//go:build integration
// +build integration

package aeron_stat

import (
	"os"
	"testing"
	"time"

	"github.com/influxdata/telegraf/config"
	"github.com/influxdata/telegraf/testutil"
	"github.com/stretchr/testify/require"
)

// TestAeronStat_IntegrationWithLiveDriver tests the plugin with a real Aeron media driver
// Run with: go test -tags=integration ./plugins/inputs/aeron_stat
func TestAeronStat_IntegrationWithLiveDriver(t *testing.T) {
	// Skip if no AERON_DIR is set
	aeronDir := os.Getenv("AERON_DIR")
	if aeronDir == "" {
		t.Skip("Skipping integration test: AERON_DIR environment variable not set")
	}

	// Check if CnC file exists
	cncFile := aeronDir + "/cnc.dat"
	if _, err := os.Stat(cncFile); os.IsNotExist(err) {
		t.Skip("Skipping integration test: CnC file not found at " + cncFile)
	}

	plugin := &AeronStat{
		AeronDir:    aeronDir,
		ReadTimeout: config.Duration(5 * time.Second),
		Tags:        map[string]string{"test": "integration"},
		Log:         testutil.Logger{},
	}

	err := plugin.Init()
	require.NoError(t, err)

	acc := &testutil.Accumulator{}

	// Start the plugin
	err = plugin.Start(acc)
	require.NoError(t, err)
	defer plugin.Stop()

	// Gather metrics
	err = plugin.Gather(acc)
	require.NoError(t, err)

	// Verify we got some metrics
	require.True(t, acc.NMetrics() > 0, "Should have collected at least one metric")

	// Check that we have summary metrics
	summaryFound := false
	for _, metric := range acc.Metrics {
		if metric.Measurement == "aeron_stat_summary" {
			summaryFound = true
			require.Contains(t, metric.Fields, "total_counters")
			require.Greater(t, metric.Fields["total_counters"], int64(0))
		}
	}
	require.True(t, summaryFound, "Should have summary metrics")

	// Verify metric structure
	for _, metric := range acc.Metrics {
		if metric.Measurement != "aeron_stat_summary" {
			// All non-summary metrics should have these tags
			require.Contains(t, metric.Tags, "counter_id")
			require.Contains(t, metric.Tags, "type_id")
			require.Contains(t, metric.Tags, "counter_type")
			require.Contains(t, metric.Tags, "test") // Our custom tag

			// All should have value and label fields
			require.Contains(t, metric.Fields, "value")
			require.Contains(t, metric.Fields, "label")
		}
	}

	t.Logf("Successfully collected %d metrics from live Aeron driver", acc.NMetrics())
}
