//go:build parsers.sbe

// Package sbe provides Simple Binary Encoding (SBE) parser for Telegraf.
package sbe

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/metric"
	"github.com/influxdata/telegraf/plugins/parsers"

	otf "github.com/influxdata/telegraf/plugins/parsers/sbe/otf"
)

// SBEParser implements telegraf.Parser interface for SBE messages.
type SBEParser struct {
	IRPath        string   `toml:"ir_path"`
	IncludeFields []string `toml:"include_fields"`
	MetricName    string   `toml:"-"`
	DefaultTags   map[string]string
	ir            *otf.IrDecoder
	once          sync.Once
	err           error
	fieldsMap     map[string]struct{}
}

// loadIR loads and parses the SBE IR file only once.
func (p *SBEParser) loadIR() error {
	p.once.Do(func() {
		if p.IRPath == "" {
			p.err = fmt.Errorf("ir_path is required")
			return
		}

		decoder := otf.NewIrDecoder()
		if decoder.DecodeFile(p.IRPath) != 0 {
			p.err = fmt.Errorf("failed to decode SBE IR file: %s", p.IRPath)
			return
		}

		p.ir = decoder
		// Build a map for fast lookup if IncludeFields is set
		if len(p.IncludeFields) > 0 {
			p.fieldsMap = make(map[string]struct{}, len(p.IncludeFields))
			for _, f := range p.IncludeFields {
				p.fieldsMap[f] = struct{}{}
			}
		}
	})
	return p.err
}

// metricListener implements otf.TokenListener and collects fields for Telegraf metrics.
type metricListener struct {
	fields     map[string]interface{}
	fieldStack []string
	filter     map[string]struct{}
}

func (l *metricListener) pushField(name string) {
	l.fieldStack = append(l.fieldStack, name)
}

func (l *metricListener) popField() {
	if len(l.fieldStack) > 0 {
		l.fieldStack = l.fieldStack[:len(l.fieldStack)-1]
	}
}

func (l *metricListener) currentPrefix() string {
	if len(l.fieldStack) == 0 {
		return ""
	}
	prefix := ""
	for i, f := range l.fieldStack {
		if i > 0 {
			prefix += "."
		}
		prefix += f
	}
	return prefix + "."
}

func (l *metricListener) shouldInclude(name string) bool {
	if l.filter == nil || len(l.filter) == 0 {
		return true
	}
	// Support dot notation for nested fields
	_, ok := l.filter[name]
	// Also allow for short field names (last part)
	if !ok && strings.Contains(name, ".") {
		parts := strings.Split(name, ".")
		_, ok = l.filter[parts[len(parts)-1]]
	}
	return ok
}

func (l *metricListener) OnBeginMessage(token otf.Token) {}

func (l *metricListener) OnEndMessage(token otf.Token) {}

func (l *metricListener) OnEncoding(fieldToken otf.Token, buffer []byte, typeToken otf.Token, actingVersion uint64) {
	name := l.currentPrefix() + fieldToken.Name()
	if !l.shouldInclude(name) {
		return
	}
	val, err := readEncodingValue(buffer, typeToken, uint64(fieldToken.TokenVersion()), actingVersion)
	if err == nil {
		l.fields[name] = val
	}
}

func (l *metricListener) OnEnum(fieldToken otf.Token, buffer []byte, tokens []otf.Token, fromIndex, toIndex int, actingVersion uint64) {
	name := l.currentPrefix() + fieldToken.Name()
	if !l.shouldInclude(name) {
		return
	}
	typeToken := tokens[fromIndex+1]
	fieldVersion := uint64(fieldToken.TokenVersion())
	var (
		enumName string
		value    interface{}
	)

	if fieldToken.IsConstantEncoding() {
		encoding := fieldToken.Encoding()
		constValue := encoding.ConstValue()
		value = primitiveValueToField(constValue)
		enumName = enumNameForConst(constValue, tokens, fromIndex, toIndex)
	} else {
		encoding := typeToken.Encoding()
		if encoding.PrimitiveType().IsUnsigned() {
			encodedValue, err := readEncodingAsUInt(buffer, typeToken, fieldVersion, actingVersion)
			if err != nil {
				return
			}
			value = encodedValue
			enumName = enumNameForUInt(encodedValue, tokens, fromIndex, toIndex)
		} else {
			encodedValue, err := readEncodingAsInt(buffer, typeToken, fieldVersion, actingVersion)
			if err != nil {
				return
			}
			value = encodedValue
			enumName = enumNameForInt(encodedValue, tokens, fromIndex, toIndex)
		}
	}

	l.fields[name] = value
	if enumName != "" {
		l.fields[name+"_enum"] = enumName
	}
}

func (l *metricListener) OnBitSet(fieldToken otf.Token, buffer []byte, tokens []otf.Token, fromIndex, toIndex int, actingVersion uint64) {
	name := l.currentPrefix() + fieldToken.Name()
	if !l.shouldInclude(name) {
		return
	}
	typeToken := tokens[fromIndex+1]
	val, err := readEncodingAsInt(buffer, typeToken, uint64(fieldToken.TokenVersion()), actingVersion)
	if err == nil {
		l.fields[name] = val
	}
}

func (l *metricListener) OnBeginComposite(fieldToken otf.Token, tokens []otf.Token, fromIndex, toIndex int) {
	l.pushField(fieldToken.Name())
}

func (l *metricListener) OnEndComposite(fieldToken otf.Token, tokens []otf.Token, fromIndex, toIndex int) {
	l.popField()
}

func (l *metricListener) OnGroupHeader(token otf.Token, numInGroup uint64) {
	name := l.currentPrefix() + token.Name() + "_count"
	if !l.shouldInclude(name) {
		return
	}
	l.fields[name] = numInGroup
}

func (l *metricListener) OnBeginGroup(token otf.Token, groupIndex, numInGroup uint64) {
	l.pushField(fmt.Sprintf("%s[%d]", token.Name(), groupIndex))
}

func (l *metricListener) OnEndGroup(token otf.Token, groupIndex, numInGroup uint64) {
	l.popField()
}

func (l *metricListener) OnVarData(fieldToken otf.Token, buffer []byte, length uint64, typeToken otf.Token) {
	name := l.currentPrefix() + fieldToken.Name()
	if !l.shouldInclude(name) {
		return
	}
	encoding := typeToken.Encoding()
	if encoding.PrimitiveType() == otf.CHAR {
		l.fields[name] = string(buffer[:length])
	} else {
		l.fields[name] = buffer[:length]
	}
}

// Parse decodes a single SBE message buffer into Telegraf metrics.
func (p *SBEParser) Parse(buf []byte) ([]telegraf.Metric, error) {
	if err := p.loadIR(); err != nil {
		return nil, err
	}

	headerDecoder, err := otf.NewOtfHeaderDecoder(p.ir.Header())
	if err != nil {
		return nil, fmt.Errorf("invalid SBE header: %w", err)
	}

	templateId, err := headerDecoder.TemplateId(buf)
	if err != nil {
		return nil, fmt.Errorf("failed to decode templateId: %w", err)
	}

	schemaId, err := headerDecoder.SchemaId(buf)
	if err != nil {
		return nil, fmt.Errorf("failed to decode schemaId: %w", err)
	}

	actingVersion, err := headerDecoder.SchemaVersion(buf)
	if err != nil {
		return nil, fmt.Errorf("failed to decode schema version: %w", err)
	}

	blockLength, err := headerDecoder.BlockLength(buf)
	if err != nil {
		return nil, fmt.Errorf("failed to decode block length: %w", err)
	}

	msgTokens := p.ir.MessageByID(int32(templateId))
	if msgTokens == nil {
		return nil, fmt.Errorf("templateId %d not found in IR", templateId)
	}

	listener := &metricListener{
		fields:     make(map[string]interface{}),
		fieldStack: []string{},
		filter:     p.fieldsMap,
	}
	msgOffset := int(headerDecoder.EncodedLength())

	_ = otf.Decode(
		buf[msgOffset:], // message body
		actingVersion,
		blockLength,
		msgTokens,
		listener,
	)

	tags := map[string]string{
		"template_id": fmt.Sprintf("%d", templateId),
		"schema_id":   fmt.Sprintf("%d", schemaId),
		"version":     fmt.Sprintf("%d", actingVersion),
	}

	metricName := p.MetricName
	if metricName == "" {
		metricName = "sbe_message"
	}

	return []telegraf.Metric{
		metric.New(metricName, mergeTags(p.DefaultTags, tags), listener.fields, time.Now().UTC()),
	}, nil
}

// ParseLine parses a single SBE message from a line buffer.
func (p *SBEParser) ParseLine(line string) (telegraf.Metric, error) {
	metrics, err := p.Parse([]byte(line))
	if err != nil {
		return nil, err
	}
	if len(metrics) == 0 {
		return nil, fmt.Errorf("no metrics parsed from line")
	}
	return metrics[0], nil
}

// SetDefaultTags is a no-op for this parser.
func (p *SBEParser) SetDefaultTags(tags map[string]string) {
	p.DefaultTags = tags
}

// Register the parser with Telegraf.
func init() {
	parsers.Add("sbe", func(defaultMetricName string) telegraf.Parser {
		return &SBEParser{MetricName: defaultMetricName}
	})
}

func mergeTags(base, extra map[string]string) map[string]string {
	if len(base) == 0 {
		return extra
	}
	if len(extra) == 0 {
		return base
	}

	out := make(map[string]string, len(base)+len(extra))
	for k, v := range base {
		out[k] = v
	}
	for k, v := range extra {
		out[k] = v
	}
	return out
}

func constOrNotPresentValue(typeToken otf.Token, fieldVersion uint64, actingVersion uint64) otf.PrimitiveValue {
	encoding := typeToken.Encoding()
	if typeToken.IsConstantEncoding() {
		return encoding.ConstValue()
	}
	if typeToken.IsOptionalEncoding() && actingVersion < fieldVersion {
		return encoding.ApplicableNullValue()
	}
	return otf.PrimitiveValue{}
}

func readEncodingValue(buffer []byte, typeToken otf.Token, fieldVersion uint64, actingVersion uint64) (interface{}, error) {
	encoding := typeToken.Encoding()
	arrayLength := typeToken.ArrayLength()
	constValue := constOrNotPresentValue(typeToken, fieldVersion, actingVersion)
	if constValue.PrimitiveType != otf.NONE {
		return primitiveValueToField(constValue), nil
	}

	if arrayLength > 1 {
		return readArrayValue(buffer, encoding, arrayLength)
	}

	switch encoding.PrimitiveType() {
	case otf.CHAR:
		if len(buffer) == 0 {
			return "", nil
		}
		return string([]byte{buffer[0]}), nil
	case otf.FLOAT, otf.DOUBLE:
		value, err := encoding.GetAsDouble(buffer)
		if err != nil {
			return nil, err
		}
		return value, nil
	default:
		if encoding.PrimitiveType().IsUnsigned() {
			value, err := encoding.GetAsUInt(buffer)
			if err != nil {
				return nil, err
			}
			return value, nil
		}
		value, err := encoding.GetAsInt(buffer)
		if err != nil {
			return nil, err
		}
		return value, nil
	}
}

func readArrayValue(buffer []byte, encoding otf.Encoding, arrayLength int32) (interface{}, error) {
	if encoding.PrimitiveType() == otf.CHAR {
		if int32(len(buffer)) < arrayLength {
			return string(buffer), nil
		}
		return string(buffer[:arrayLength]), nil
	}

	elementSize := int32(encoding.PrimitiveType().Size())
	if elementSize == 0 {
		return "", nil
	}
	if int32(len(buffer)) < arrayLength*elementSize {
		return "", fmt.Errorf("buffer too short for array value")
	}

	values := make([]string, 0, arrayLength)
	for i := int32(0); i < arrayLength; i++ {
		start := i * elementSize
		end := start + elementSize
		switch encoding.PrimitiveType() {
		case otf.FLOAT, otf.DOUBLE:
			value, err := encoding.GetAsDouble(buffer[start:end])
			if err != nil {
				return nil, err
			}
			values = append(values, fmt.Sprint(value))
		default:
			if encoding.PrimitiveType().IsUnsigned() {
				value, err := encoding.GetAsUInt(buffer[start:end])
				if err != nil {
					return nil, err
				}
				values = append(values, fmt.Sprint(value))
			} else {
				value, err := encoding.GetAsInt(buffer[start:end])
				if err != nil {
					return nil, err
				}
				values = append(values, fmt.Sprint(value))
			}
		}
	}

	return "[" + strings.Join(values, ",") + "]", nil
}

func readEncodingAsUInt(buffer []byte, typeToken otf.Token, fieldVersion uint64, actingVersion uint64) (uint64, error) {
	constValue := constOrNotPresentValue(typeToken, fieldVersion, actingVersion)
	if constValue.PrimitiveType != otf.NONE {
		return constValue.AsUInt(), nil
	}
	encoding := typeToken.Encoding()
	return encoding.GetAsUInt(buffer)
}

func readEncodingAsInt(buffer []byte, typeToken otf.Token, fieldVersion uint64, actingVersion uint64) (int64, error) {
	constValue := constOrNotPresentValue(typeToken, fieldVersion, actingVersion)
	if constValue.PrimitiveType != otf.NONE {
		return constValue.AsInt(), nil
	}
	encoding := typeToken.Encoding()
	return encoding.GetAsInt(buffer)
}

func enumNameForUInt(encodedValue uint64, tokens []otf.Token, fromIndex, toIndex int) string {
	for i := fromIndex + 1; i < toIndex; i++ {
		encoding := tokens[i].Encoding()
		constValue := encoding.ConstValue()
		if constValue.AsUInt() == encodedValue {
			return tokens[i].Name()
		}
	}
	return ""
}

func enumNameForInt(encodedValue int64, tokens []otf.Token, fromIndex, toIndex int) string {
	for i := fromIndex + 1; i < toIndex; i++ {
		encoding := tokens[i].Encoding()
		constValue := encoding.ConstValue()
		if constValue.AsInt() == encodedValue {
			return tokens[i].Name()
		}
	}
	return ""
}

func enumNameForConst(constValue otf.PrimitiveValue, tokens []otf.Token, fromIndex, toIndex int) string {
	if constValue.PrimitiveType.IsUnsigned() {
		return enumNameForUInt(constValue.AsUInt(), tokens, fromIndex, toIndex)
	}
	return enumNameForInt(constValue.AsInt(), tokens, fromIndex, toIndex)
}

func primitiveValueToField(value otf.PrimitiveValue) interface{} {
	switch value.PrimitiveType {
	case otf.CHAR:
		return value.AsString()
	case otf.FLOAT, otf.DOUBLE:
		return value.AsDouble()
	default:
		if value.PrimitiveType.IsUnsigned() {
			return value.AsUInt()
		}
		return value.AsInt()
	}
}
