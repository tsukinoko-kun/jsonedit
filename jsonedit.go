package jsonedit

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"reflect"
	"strings"
	"unicode"
)

// Format represents detected JSON formatting
type Format struct {
	Indent          string
	Prefix          string
	Compact         bool
	SpaceAfterColon bool
	SpaceAfterComma bool
	TrailingNewline bool
}

// OrderedValue preserves the order and type of JSON values
type OrderedValue struct {
	Order int
	Value interface{}
}

// OrderedMap preserves key order in JSON objects
type OrderedMap struct {
	Keys   []string
	Values map[string]*OrderedValue
}

// NewOrderedMap creates a new OrderedMap
func NewOrderedMap() *OrderedMap {
	return &OrderedMap{
		Keys:   []string{},
		Values: make(map[string]*OrderedValue),
	}
}

// Set adds or updates a key-value pair
func (om *OrderedMap) Set(key string, value interface{}, order int) {
	if _, exists := om.Values[key]; !exists {
		om.Keys = append(om.Keys, key)
	}
	om.Values[key] = &OrderedValue{Order: order, Value: value}
}

// Get retrieves a value by key
func (om *OrderedMap) Get(key string) (interface{}, bool) {
	if ov, ok := om.Values[key]; ok {
		return ov.Value, true
	}
	return nil, false
}

// Document represents a parsed JSON document with formatting preserved
type Document[T interface{}] struct {
	TypedData   T
	Rest        *OrderedMap
	Format      Format
	OriginalMap *OrderedMap
}

// String serializes the document to a JSON string
func (d *Document[T]) String() (string, error) {
	var buf bytes.Buffer
	if err := d.Write(&buf); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// Write serializes the document to an io.Writer
func (d *Document[T]) Write(w io.Writer) error {
	merged := d.mergeInOriginalOrder()
	encoder := d.createEncoder(w)
	if err := encoder.encode(merged, 0); err != nil {
		return err
	}

	// Add trailing newline if present in original
	if d.Format.TrailingNewline {
		_, err := w.Write([]byte("\n"))
		return err
	}

	return nil
}

func isNil[T any](x T) bool {
	v := reflect.ValueOf(x)
	// reflect.ValueOf(nil) yields zero Value — treat as nil
	if !v.IsValid() {
		return true
	}
	switch v.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface,
		reflect.Map, reflect.Ptr, reflect.Slice:
		return v.IsNil()
	default:
		return false
	}
}

// mergeInOriginalOrder merges typed and rest data in the original order
func (d *Document[T]) mergeInOriginalOrder() interface{} {
	if d.OriginalMap == nil {
		return nil
	}

	result := NewOrderedMap()

	// Get typed fields mapping
	typedFields := make(map[string]interface{})
	if !isNil(d.TypedData) {
		v := reflect.ValueOf(d.TypedData)
		if v.Kind() == reflect.Pointer {
			v = v.Elem()
		}
		t := v.Type()

		if v.Kind() == reflect.Struct {
			for i := 0; i < v.NumField(); i++ {
				field := t.Field(i)
				fieldValue := v.Field(i)

				jsonTag := field.Tag.Get("json")
				if jsonTag == "-" {
					continue
				}

				name := field.Name
				if jsonTag != "" {
					parts := strings.Split(jsonTag, ",")
					if parts[0] != "" {
						name = parts[0]
					}

					if strings.Contains(jsonTag, "omitempty") &&
						isEmptyValue(fieldValue) {
						continue
					}
				}

				typedFields[name] = fieldValue.Interface()
			}
		}
	}

	// Iterate through original order
	for _, key := range d.OriginalMap.Keys {
		if typedVal, ok := typedFields[key]; ok {
			// Check if the original value was an OrderedMap (nested object)
			// and the typed value is a map
			if origVal, origOk := d.OriginalMap.Get(key); origOk {
				if origMap, isOrderedMap := origVal.(*OrderedMap); isOrderedMap {
					if typedMap, isMap := typedVal.(map[string]string); isMap {
						// Merge typed map into ordered map preserving order
						mergedMap := NewOrderedMap()
						// First add existing keys that are still in typed map (preserving order)
						for _, origKey := range origMap.Keys {
							if val, exists := typedMap[origKey]; exists {
								mergedMap.Set(origKey, val, len(mergedMap.Keys))
							}
							// Don't add keys that were deleted from typed map
						}
						// Then add any new keys from typed map
						for typedKey, typedValue := range typedMap {
							if _, exists := mergedMap.Values[typedKey]; !exists {
								mergedMap.Set(typedKey, typedValue, len(mergedMap.Keys))
							}
						}
						result.Set(key, mergedMap, len(result.Keys))
					} else {
						// Not a string map, use typed value as-is
						result.Set(key, typedVal, len(result.Keys))
					}
				} else {
					// Original wasn't an ordered map, use typed value
					result.Set(key, typedVal, len(result.Keys))
				}
			} else {
				// No original value, use typed value
				result.Set(key, typedVal, len(result.Keys))
			}
		} else if d.Rest != nil {
			// Use rest value
			if val, ok := d.Rest.Get(key); ok {
				result.Set(key, val, len(result.Keys))
			}
		} else {
			// Use original value if no typed or rest value
			if val, ok := d.OriginalMap.Get(key); ok {
				result.Set(key, val, len(result.Keys))
			}
		}
	}

	return result
}

// customEncoder handles ordered serialization
type customEncoder struct {
	w      io.Writer
	format Format
}

func (d *Document[T]) createEncoder(w io.Writer) *customEncoder {
	return &customEncoder{
		w:      w,
		format: d.Format,
	}
}

func (ce *customEncoder) encode(v interface{}, depth int) error {
	switch val := v.(type) {
	case *OrderedMap:
		return ce.encodeOrderedMap(val, depth)
	case map[string]string:
		return ce.encodeMap(val, depth)
	case map[string]interface{}:
		return ce.encodeGenericMap(val, depth)
	case []interface{}:
		return ce.encodeArray(val, depth)
	case string:
		data, _ := json.Marshal(val)
		_, err := ce.w.Write(data)
		return err
	case float64, bool, nil:
		data, _ := json.Marshal(val)
		_, err := ce.w.Write(data)
		return err
	default:
		rv := reflect.ValueOf(val)
		if rv.Kind() == reflect.Struct ||
			(rv.Kind() == reflect.Pointer && rv.Elem().Kind() == reflect.Struct) {
			return ce.encodeStruct(rv, depth)
		}
		data, err := json.Marshal(val)
		if err != nil {
			return err
		}
		_, err = ce.w.Write(data)
		return err
	}
}

func (ce *customEncoder) encodeOrderedMap(om *OrderedMap, depth int) error {
	ce.w.Write([]byte("{"))

	for i, key := range om.Keys {
		if i > 0 {
			ce.w.Write([]byte(","))
			if ce.format.SpaceAfterComma {
				ce.w.Write([]byte(" "))
			}
		}

		if !ce.format.Compact {
			ce.w.Write([]byte("\n"))
			ce.w.Write([]byte(strings.Repeat(ce.format.Indent, depth+1)))
		}

		// Write key
		keyData, _ := json.Marshal(key)
		ce.w.Write(keyData)
		ce.w.Write([]byte(":"))

		if ce.format.SpaceAfterColon {
			ce.w.Write([]byte(" "))
		}

		// Write value
		if ov, ok := om.Values[key]; ok {
			if err := ce.encode(ov.Value, depth+1); err != nil {
				return err
			}
		}
	}

	if !ce.format.Compact && len(om.Keys) > 0 {
		ce.w.Write([]byte("\n"))
		ce.w.Write([]byte(strings.Repeat(ce.format.Indent, depth)))
	}

	ce.w.Write([]byte("}"))
	return nil
}

func (ce *customEncoder) encodeMap(m map[string]string, depth int) error {
	// Convert to OrderedMap to maintain order if possible
	om := NewOrderedMap()
	i := 0
	for k, v := range m {
		om.Set(k, v, i)
		i++
	}
	return ce.encodeOrderedMap(om, depth)
}

func (ce *customEncoder) encodeGenericMap(m map[string]interface{}, depth int) error {
	om := NewOrderedMap()
	i := 0
	for k, v := range m {
		om.Set(k, v, i)
		i++
	}
	return ce.encodeOrderedMap(om, depth)
}

func (ce *customEncoder) encodeArray(arr []interface{}, depth int) error {
	ce.w.Write([]byte("["))

	for i, item := range arr {
		if i > 0 {
			ce.w.Write([]byte(","))
			if ce.format.SpaceAfterComma {
				ce.w.Write([]byte(" "))
			}
		}

		if !ce.format.Compact {
			ce.w.Write([]byte("\n"))
			ce.w.Write([]byte(strings.Repeat(ce.format.Indent, depth+1)))
		}

		if err := ce.encode(item, depth+1); err != nil {
			return err
		}
	}

	if !ce.format.Compact && len(arr) > 0 {
		ce.w.Write([]byte("\n"))
		ce.w.Write([]byte(strings.Repeat(ce.format.Indent, depth)))
	}

	ce.w.Write([]byte("]"))
	return nil
}

func (ce *customEncoder) encodeStruct(v reflect.Value, depth int) error {
	if v.Kind() == reflect.Pointer {
		v = v.Elem()
	}

	t := v.Type()
	om := NewOrderedMap()

	for i := 0; i < v.NumField(); i++ {
		field := t.Field(i)
		fieldValue := v.Field(i)

		jsonTag := field.Tag.Get("json")
		if jsonTag == "-" {
			continue
		}

		name := field.Name
		if jsonTag != "" {
			parts := strings.Split(jsonTag, ",")
			if parts[0] != "" {
				name = parts[0]
			}

			if strings.Contains(jsonTag, "omitempty") && isEmptyValue(fieldValue) {
				continue
			}
		}

		om.Set(name, fieldValue.Interface(), i)
	}

	return ce.encodeOrderedMap(om, depth)
}

// Parse reads JSON from reader and parses it into typed and untyped data
func Parse[T interface{}](r io.Reader, typedData T) (*Document[T], error) {
	// Read all data
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}

	// Detect format
	format := detectFormat(data)

	// Parse JSON with order preservation
	ordered, err := parseOrdered(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}

	doc := &Document[T]{
		TypedData:   typedData,
		Format:      format,
		OriginalMap: ordered,
	}

	// If typedData is provided, unmarshal into it
	if !isNil(typedData) {
		if err := json.Unmarshal(data, typedData); err != nil {
			return nil, err
		}

		// Extract rest fields
		doc.Rest = extractRest(ordered, typedData)
	} else {
		// No typed data, everything goes to rest
		doc.Rest = ordered
	}

	return doc, nil
}

// parseOrdered parses JSON preserving key order
func parseOrdered(r io.Reader) (*OrderedMap, error) {
	decoder := json.NewDecoder(r)
	decoder.UseNumber()

	t, err := decoder.Token()
	if err != nil {
		return nil, err
	}

	if t != json.Delim('{') {
		return nil, fmt.Errorf("expected object, got %v", t)
	}

	return parseObject(decoder)
}

// parseObject parses a JSON object preserving key order
func parseObject(decoder *json.Decoder) (*OrderedMap, error) {
	om := NewOrderedMap()
	order := 0

	for {
		t, err := decoder.Token()
		if err != nil {
			return nil, err
		}

		if t == json.Delim('}') {
			break
		}

		key, ok := t.(string)
		if !ok {
			return nil, fmt.Errorf("expected string key, got %v", t)
		}

		value, err := parseValue(decoder)
		if err != nil {
			return nil, err
		}

		om.Set(key, value, order)
		order++
	}

	return om, nil
}

// parseValue parses any JSON value
func parseValue(decoder *json.Decoder) (interface{}, error) {
	t, err := decoder.Token()
	if err != nil {
		return nil, err
	}

	switch v := t.(type) {
	case json.Delim:
		switch v {
		case json.Delim('{'):
			return parseObject(decoder)
		case json.Delim('['):
			return parseArray(decoder)
		default:
			return nil, fmt.Errorf("unexpected delimiter: %v", v)
		}
	case string:
		return v, nil
	case json.Number:
		// Try to convert to appropriate numeric type
		if i, err := v.Int64(); err == nil {
			return float64(i), nil
		}
		if f, err := v.Float64(); err == nil {
			return f, nil
		}
		return v.String(), nil
	case bool:
		return v, nil
	case nil:
		return nil, nil
	default:
		return v, nil
	}
}

// parseArray parses a JSON array
func parseArray(decoder *json.Decoder) ([]interface{}, error) {
	var arr []interface{}

	for {
		if !decoder.More() {
			// Read closing bracket
			t, err := decoder.Token()
			if err != nil {
				return nil, err
			}
			if t != json.Delim(']') {
				return nil, fmt.Errorf("expected ], got %v", t)
			}
			break
		}

		value, err := parseValue(decoder)
		if err != nil {
			return nil, err
		}

		arr = append(arr, value)
	}

	return arr, nil
}

// detectFormat analyzes JSON formatting
func detectFormat(data []byte) Format {
	format := Format{
		Compact:         true,
		SpaceAfterColon: false,
		SpaceAfterComma: false,
	}

	// Check for newlines (non-compact)
	if bytes.Contains(data, []byte("\n")) {
		format.Compact = false

		// Detect indentation
		lines := bytes.Split(data, []byte("\n"))
		for i, line := range lines {
			if i == 0 {
				continue
			}

			trimmed := bytes.TrimLeftFunc(line, unicode.IsSpace)
			if len(trimmed) > 0 && len(line) > len(trimmed) {
				indent := line[:len(line)-len(trimmed)]
				if bytes.HasPrefix(indent, []byte("  ")) {
					format.Indent = "  "
				} else if bytes.HasPrefix(indent, []byte("    ")) {
					format.Indent = "    "
				} else if bytes.HasPrefix(indent, []byte("\t")) {
					format.Indent = "\t"
				}
				break
			}
		}
	}

	// Check for space after colon
	if bytes.Contains(data, []byte(": ")) {
		format.SpaceAfterColon = true
	}

	// Check for space after comma
	if bytes.Contains(data, []byte(", ")) {
		format.SpaceAfterComma = true
	}

	if len(data) > 0 && data[len(data)-1] == '\n' {
		format.TrailingNewline = true
	}

	return format
}

// extractRest extracts fields not present in typed data
func extractRest(om *OrderedMap, typedData interface{}) *OrderedMap {
	rest := NewOrderedMap()

	v := reflect.ValueOf(typedData)
	if v.Kind() == reflect.Pointer {
		v = v.Elem()
	}

	if v.Kind() != reflect.Struct {
		return om
	}

	t := v.Type()
	typedFields := make(map[string]bool)

	// Collect all typed field names
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		jsonTag := field.Tag.Get("json")

		if jsonTag == "-" {
			continue
		}

		name := field.Name
		if jsonTag != "" {
			parts := strings.Split(jsonTag, ",")
			if parts[0] != "" {
				name = parts[0]
			}
		}

		typedFields[name] = true
	}

	// Add non-typed fields to rest
	for _, key := range om.Keys {
		if !typedFields[key] {
			if ov, ok := om.Values[key]; ok {
				rest.Set(key, ov.Value, ov.Order)
			}
		}
	}

	return rest
}

// isEmptyValue checks if a reflect.Value is empty
func isEmptyValue(v reflect.Value) bool {
	switch v.Kind() {
	case reflect.Array, reflect.Map, reflect.Slice, reflect.String:
		return v.Len() == 0
	case reflect.Bool:
		return !v.Bool()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return v.Int() == 0
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return v.Uint() == 0
	case reflect.Float32, reflect.Float64:
		return v.Float() == 0
	case reflect.Interface, reflect.Pointer:
		return v.IsNil()
	}
	return false
}
