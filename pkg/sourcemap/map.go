package sourcemap

import (
	"strings"

	"encoding/json/v2"
)

// Map is a source map kept as JSON text with a lazily decoded view.
type Map struct {
	text     string
	decoded  *decodedMap
	fallback string
}

// ParseMap validates map JSON; an empty string means no map.
func ParseMap(text string) (*Map, error) {
	if strings.TrimSpace(text) == "" {
		return nil, nil
	}
	var raw map[string]any
	if err := json.Unmarshal([]byte(text), &raw); err != nil {
		return nil, err
	}
	return &Map{text: text}, nil
}

func newMap(text string) *Map {
	if strings.TrimSpace(text) == "" {
		return nil
	}
	return &Map{text: text}
}

// JSON returns the map as source map JSON, or an empty string when absent.
func (m *Map) JSON() string {
	if m == nil {
		return ""
	}
	return m.text
}

// IsZero reports whether the map is absent.
func (m *Map) IsZero() bool {
	return m == nil || strings.TrimSpace(m.text) == ""
}

func (m *Map) decode(fallback string) *decodedMap {
	if m == nil {
		return nil
	}
	if m.decoded == nil || m.fallback != fallback {
		m.decoded = decodeMap(m.text, fallback)
		m.fallback = fallback
	}
	return m.decoded
}
