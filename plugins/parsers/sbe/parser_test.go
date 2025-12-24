//go:build parsers.sbe

package sbe

import (
	"encoding/binary"
	"path/filepath"
	"testing"
)

func TestParseMinimalMessage(t *testing.T) {
	parser := &SBEParser{
		IRPath: filepath.Join("testdata", "minimal-schema.sbeir"),
	}

	buf := make([]byte, 8)
	binary.LittleEndian.PutUint16(buf[0:2], 0) // blockLength
	binary.LittleEndian.PutUint16(buf[2:4], 1) // templateId
	binary.LittleEndian.PutUint16(buf[4:6], 1) // schemaId
	binary.LittleEndian.PutUint16(buf[6:8], 0) // version

	metrics, err := parser.Parse(buf)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	if len(metrics) != 1 {
		t.Fatalf("expected 1 metric, got %d", len(metrics))
	}

	metric := metrics[0]
	if metric.Name() != "sbe_message" {
		t.Fatalf("unexpected metric name: %s", metric.Name())
	}

	tags := metric.Tags()
	if tags["template_id"] != "1" {
		t.Fatalf("unexpected template_id tag: %q", tags["template_id"])
	}
	if tags["schema_id"] != "1" {
		t.Fatalf("unexpected schema_id tag: %q", tags["schema_id"])
	}
	if tags["version"] != "0" {
		t.Fatalf("unexpected version tag: %q", tags["version"])
	}

	if len(metric.Fields()) != 0 {
		t.Fatalf("expected no fields, got %v", metric.Fields())
	}
}

func TestParseExtendedMessage(t *testing.T) {
	parser := &SBEParser{
		IRPath: filepath.Join("testdata", "extended-schema.sbeir"),
	}

	buf := make([]byte, 0, 64)
	header := make([]byte, 8)
	binary.LittleEndian.PutUint16(header[0:2], 10) // blockLength
	binary.LittleEndian.PutUint16(header[2:4], 1)  // templateId
	binary.LittleEndian.PutUint16(header[4:6], 1)  // schemaId
	binary.LittleEndian.PutUint16(header[6:8], 0)  // version
	buf = append(buf, header...)

	body := make([]byte, 10)
	binary.LittleEndian.PutUint32(body[0:4], 42) // id
	binary.LittleEndian.PutUint16(body[4:6], 2)  // status WARN
	binary.LittleEndian.PutUint16(body[6:8], 7)  // point.x
	binary.LittleEndian.PutUint16(body[8:10], 9) // point.y
	buf = append(buf, body...)

	groupHeader := make([]byte, 4)
	binary.LittleEndian.PutUint16(groupHeader[0:2], 2) // group blockLength
	binary.LittleEndian.PutUint16(groupHeader[2:4], 2) // numInGroup
	buf = append(buf, groupHeader...)

	groupEntry := make([]byte, 2)
	binary.LittleEndian.PutUint16(groupEntry, 100)
	buf = append(buf, groupEntry...)
	binary.LittleEndian.PutUint16(groupEntry, 110)
	buf = append(buf, groupEntry...)

	note := []byte("hello")
	noteHeader := make([]byte, 4)
	binary.LittleEndian.PutUint32(noteHeader, uint32(len(note)))
	buf = append(buf, noteHeader...)
	buf = append(buf, note...)

	metrics, err := parser.Parse(buf)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	if len(metrics) != 1 {
		t.Fatalf("expected 1 metric, got %d", len(metrics))
	}

	fields := metrics[0].Fields()
	if fields["id"] != uint64(42) {
		t.Fatalf("unexpected id field: %v", fields["id"])
	}
	if fields["status"] != uint64(2) {
		t.Fatalf("unexpected status field: %v", fields["status"])
	}
	if fields["status_enum"] != "WARN" {
		t.Fatalf("unexpected status_enum field: %v", fields["status_enum"])
	}
	if fields["point.x"] != int64(7) {
		t.Fatalf("unexpected point.x field: %v", fields["point.x"])
	}
	if fields["point.y"] != int64(9) {
		t.Fatalf("unexpected point.y field: %v", fields["point.y"])
	}
	if fields["readings_count"] != uint64(2) {
		t.Fatalf("unexpected readings_count field: %v", fields["readings_count"])
	}
	if fields["readings[0].speed"] != uint64(100) {
		t.Fatalf("unexpected readings[0].speed field: %v", fields["readings[0].speed"])
	}
	if fields["readings[1].speed"] != uint64(110) {
		t.Fatalf("unexpected readings[1].speed field: %v", fields["readings[1].speed"])
	}
	if fields["note"] != "hello" {
		t.Fatalf("unexpected note field: %v", fields["note"])
	}
}
