package aeron_stat

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/config"
	"github.com/influxdata/telegraf/plugins/inputs"
	"github.com/lirm/aeron-go/aeron"
	"github.com/lirm/aeron-go/aeron/counters"
	"github.com/lirm/aeron-go/aeron/util/memmap"
)

// AeronStat implements the telegraf.Input interface to collect Aeron CnC file metrics
type AeronStat struct {
	AeronDir    string            `toml:"aeron_dir"`
	ReadTimeout config.Duration   `toml:"read_timeout"`
	Tags        map[string]string `toml:"tags"`
	Log         telegraf.Logger   `toml:"-"`

	// Internal fields
	reader      *counters.Reader
	counterFile *counters.MetaDataFlyweight
	cncFile     *memmap.File
}

// Description returns a description of the plugin
func (a *AeronStat) Description() string {
	return "Collect Aeron media driver CnC file metrics"
}

// SampleConfig returns a sample configuration for the plugin
func (a *AeronStat) SampleConfig() string {
	return `
  ## Aeron directory path where CnC files are located
  ## Default: system default (usually /dev/shm/aeron or /tmp/aeron)
  # aeron_dir = "/dev/shm/aeron"
  
  ## Timeout for reading CnC files
  # read_timeout = "5s"
  
  ## Add custom tags to all metrics
  # [inputs.aeron_stat.tags]
  #   environment = "production"
  #   datacenter = "us-west-1"
`
}

// Init initializes the plugin with default values
func (a *AeronStat) Init() error {
	if a.ReadTimeout == 0 {
		a.ReadTimeout = config.Duration(5 * time.Second)
	}

	// If no aeron_dir specified, use the default from Aeron
	if a.AeronDir == "" {
		// Use the same default as NewContext() does
		a.AeronDir = aeron.DefaultAeronDir + "/aeron-" + aeron.UserName
		a.Log.Debugf("Using default Aeron directory: %s", a.AeronDir)
	}

	return nil
}

// Start initializes the CnC file reader
func (a *AeronStat) Start(acc telegraf.Accumulator) error {
	a.Log.Infof("Starting Aeron stat collection from directory: %s", a.AeronDir)

	// Initialize the CnC file reader
	err := a.initializeReader()
	if err != nil {
		return fmt.Errorf("failed to initialize CnC reader: %w", err)
	}

	a.Log.Info("Aeron stat plugin started successfully")
	return nil
}

// Stop cleans up resources
func (a *AeronStat) Stop() {
	a.Log.Info("Stopping Aeron stat plugin")
	a.cleanup()
}

// Gather collects metrics from the Aeron CnC files
func (a *AeronStat) Gather(acc telegraf.Accumulator) error {
	// Ensure reader is initialized
	if a.reader == nil {
		err := a.initializeReader()
		if err != nil {
			a.Log.Errorf("Failed to initialize CnC reader: %v", err)
			return err
		}
	}

	// Collect counter metrics
	err := a.collectCounters(acc)
	if err != nil {
		a.Log.Errorf("Failed to collect counters: %v", err)
		// Try to reinitialize on next gather
		a.cleanup()
		return err
	}

	return nil
}

// initializeReader sets up the CnC file reader
func (a *AeronStat) initializeReader() error {
	// Clean up any existing resources
	a.cleanup()

	// Get the CnC file path
	ctx := aeron.NewContext()
	if a.AeronDir != "" {
		ctx.AeronDir(a.AeronDir)
	}
	cncFileName := ctx.CncFileName()

	a.Log.Debugf("Opening CnC file: %s", cncFileName)

	// Map the CnC file
	counterFile, cncFile, err := counters.MapFile(cncFileName)
	if err != nil {
		return fmt.Errorf("failed to map CnC file %s: %w", cncFileName, err)
	}

	// Create counter reader
	reader := counters.NewReader(counterFile.ValuesBuf.Get(), counterFile.MetaDataBuf.Get())

	// Store references
	a.counterFile = counterFile
	a.cncFile = cncFile
	a.reader = reader

	a.Log.Debugf("Successfully initialized CnC reader")
	return nil
}

// collectCounters gathers all counter metrics
func (a *AeronStat) collectCounters(acc telegraf.Accumulator) error {
	if a.reader == nil {
		return fmt.Errorf("counter reader not initialized")
	}

	counterCount := 0

	// Scan all counters and convert to metrics
	a.reader.Scan(func(counter counters.Counter) {
		counterCount++

		// Create base tags
		tags := make(map[string]string)

		// Add configured tags
		for key, value := range a.Tags {
			tags[key] = value
		}

		// Add counter-specific tags
		tags["counter_id"] = fmt.Sprintf("%d", counter.Id)
		tags["type_id"] = fmt.Sprintf("%d", counter.TypeId)

		// Parse counter type name and add structured information
		counterType := a.parseCounterType(counter.TypeId)
		if counterType != "" {
			tags["counter_type"] = counterType
		}

		// Parse label for additional structured information
		parsedLabel := a.parseCounterLabel(counter.Label)
		for key, value := range parsedLabel.tags {
			tags[key] = value
		}

		// Create fields with parsed label information
		fields := map[string]interface{}{
			"value": counter.Value,
		}

		// Add parsed fields from label
		for key, value := range parsedLabel.fields {
			fields[key] = value
		}

		// Determine measurement name based on counter type
		measurement := a.getMeasurementName(counter.TypeId, counterType)

		// Add the metric
		acc.AddFields(measurement, fields, tags)
	})

	// Add summary metric
	summaryTags := make(map[string]string)
	for key, value := range a.Tags {
		summaryTags[key] = value
	}

	summaryFields := map[string]interface{}{
		"total_counters": counterCount,
	}

	acc.AddFields("aeron_stat_summary", summaryFields, summaryTags)

	a.Log.Debugf("Collected %d counters", counterCount)
	return nil
}

// cleanup releases resources
func (a *AeronStat) cleanup() {
	if a.cncFile != nil {
		a.cncFile.Close()
		a.cncFile = nil
	}
	a.counterFile = nil
	a.reader = nil
}

// ParsedLabel holds structured information extracted from counter labels
type ParsedLabel struct {
	tags   map[string]string
	fields map[string]interface{}
}

// parseCounterType returns a human-readable counter type name based on type ID
func (a *AeronStat) parseCounterType(typeId int32) string {
	// Common Aeron counter type IDs - these come from the Aeron source code
	switch typeId {
	case 0:
		return "system_counter"
	case 1:
		return "bytes_sent"
	case 2:
		return "bytes_received"
	case 3:
		return "failed_offers"
	case 4:
		return "nak_messages_sent"
	case 5:
		return "nak_messages_received"
	case 6:
		return "status_messages_sent"
	case 7:
		return "status_messages_received"
	case 8:
		return "heartbeats_sent"
	case 9:
		return "heartbeats_received"
	case 10:
		return "retransmits_sent"
	case 11:
		return "flow_control_under_runs"
	case 12:
		return "flow_control_over_runs"
	case 13:
		return "invalid_packets"
	case 14:
		return "errors"
	case 15:
		return "short_sends"
	case 16:
		return "free_fails"
	case 17:
		return "sender_flow_control_limits"
	case 18:
		return "unblocked_publications"
	case 19:
		return "unblocked_control_commands"
	case 20:
		return "possible_ttl_asymmetry"
	case 21:
		return "reception_rate"
	case 22:
		return "sender_bpe"
	case 23:
		return "receiver_hwm"
	case 24:
		return "receiver_pos"
	case 25:
		return "sender_pos"
	case 26:
		return "sender_limit"
	case 27:
		return "per_image_type_id"
	case 28:
		return "publication_limit"
	case 29:
		return "sender_bpe_manual"
	case 30:
		return "subscription_pos"
	case 31:
		return "stream_pos"
	case 32:
		return "publisher_pos"
	case 33:
		return "publisher_limit"
	case 34:
		return "client_heartbeat"
	case 35:
		return "client_keepalive"
	default:
		return fmt.Sprintf("unknown_%d", typeId)
	}
}

// parseCounterLabel extracts structured information from counter labels
func (a *AeronStat) parseCounterLabel(label string) ParsedLabel {
	parsed := ParsedLabel{
		tags:   make(map[string]string),
		fields: make(map[string]interface{}),
	}

	// Store the original label
	parsed.fields["label"] = label

	// Parse channel information if present
	if strings.Contains(label, "aeron:") {
		// Extract channel info
		if idx := strings.Index(label, "aeron:"); idx >= 0 {
			channelPart := label[idx:]
			if spaceIdx := strings.Index(channelPart, " "); spaceIdx > 0 {
				channelPart = channelPart[:spaceIdx]
			}
			parsed.tags["channel"] = channelPart

			// Parse endpoint if present
			if strings.Contains(channelPart, "endpoint=") {
				endpointStart := strings.Index(channelPart, "endpoint=") + 9
				endpointEnd := strings.Index(channelPart[endpointStart:], "&")
				if endpointEnd == -1 {
					endpointEnd = len(channelPart[endpointStart:])
				}
				endpoint := channelPart[endpointStart : endpointStart+endpointEnd]
				parsed.tags["endpoint"] = endpoint
			}
		}
	}

	// Parse stream ID if present
	if strings.Contains(label, "stream-id=") {
		streamStart := strings.Index(label, "stream-id=") + 10
		streamEnd := strings.Index(label[streamStart:], " ")
		if streamEnd == -1 {
			streamEnd = len(label[streamStart:])
		}
		streamIdStr := label[streamStart : streamStart+streamEnd]
		if streamId, err := strconv.ParseInt(streamIdStr, 10, 64); err == nil {
			parsed.fields["stream_id"] = streamId
		}
	}

	// Parse session ID if present
	if strings.Contains(label, "session-id=") {
		sessionStart := strings.Index(label, "session-id=") + 11
		sessionEnd := strings.Index(label[sessionStart:], " ")
		if sessionEnd == -1 {
			sessionEnd = len(label[sessionStart:])
		}
		sessionIdStr := label[sessionStart : sessionStart+sessionEnd]
		if sessionId, err := strconv.ParseInt(sessionIdStr, 10, 64); err == nil {
			parsed.fields["session_id"] = sessionId
		}
	}

	return parsed
}

// getMeasurementName determines the appropriate measurement name for a counter
func (a *AeronStat) getMeasurementName(typeId int32, counterType string) string {
	// Group related counters into logical measurements
	switch {
	case typeId >= 1 && typeId <= 2: // bytes sent/received
		return "aeron_bytes"
	case typeId >= 4 && typeId <= 10: // various message types
		return "aeron_messages"
	case typeId >= 11 && typeId <= 20: // flow control and errors
		return "aeron_flow_control"
	case typeId >= 21 && typeId <= 26: // position tracking
		return "aeron_positions"
	case typeId >= 27 && typeId <= 33: // publication/subscription tracking
		return "aeron_streams"
	case typeId >= 34 && typeId <= 35: // client lifecycle
		return "aeron_clients"
	case typeId == 0: // system counters
		return "aeron_system"
	default:
		return "aeron_counter"
	}
}

func init() {
	inputs.Add("aeron_stat", func() telegraf.Input {
		return &AeronStat{
			ReadTimeout: config.Duration(5 * time.Second),
		}
	})
}
