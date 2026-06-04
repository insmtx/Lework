package llmprotocol

import (
	"time"

	"github.com/bytedance/sonic"
)

// getString safely extracts a string from a map.
func getString(m map[string]interface{}, key string) string {
	if v, ok := m[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

// getBool safely extracts a bool from a map.
func getBool(m map[string]interface{}, key string) bool {
	if v, ok := m[key]; ok {
		if b, ok := v.(bool); ok {
			return b
		}
	}
	return false
}

// getFloat safely extracts a float64 from a map.
func getFloat(m map[string]interface{}, key string) (float64, bool) {
	if v, ok := m[key]; ok {
		switch n := v.(type) {
		case float64:
			return n, true
		case int:
			return float64(n), true
		}
	}
	return 0, false
}

// getInt safely extracts an int from a map.
func getInt(m map[string]interface{}, key string) (int, bool) {
	if v, ok := m[key]; ok {
		switch n := v.(type) {
		case float64:
			return int(n), true
		case int:
			return n, true
		case int64:
			return int(n), true
		}
	}
	return 0, false
}

// getIntDefault extracts an int or returns 0.
func getIntDefault(m map[string]interface{}, key string) int {
	if v, ok := getInt(m, key); ok {
		return v
	}
	return 0
}

// getInt64 safely extracts an int64 from a map.
func getInt64(m map[string]interface{}, key string) int64 {
	if v, ok := m[key]; ok {
		switch n := v.(type) {
		case float64:
			return int64(n)
		case int64:
			return n
		case int:
			return int64(n)
		}
	}
	return 0
}

// getList safely extracts a []interface{} from a map.
func getList(m map[string]interface{}, key string) ([]interface{}, bool) {
	if v, ok := m[key]; ok {
		if l, ok := v.([]interface{}); ok {
			return l, true
		}
	}
	return nil, false
}

// getStringList safely extracts a []string from a map.
func getStringList(m map[string]interface{}, key string) ([]string, bool) {
	if v, ok := m[key]; ok {
		switch l := v.(type) {
		case []interface{}:
			var ss []string
			for _, item := range l {
				if s, ok := item.(string); ok {
					ss = append(ss, s)
				}
			}
			return ss, true
		case []string:
			return l, true
		}
	}
	return nil, false
}

// contentToString converts content to string (handles string, []interface{}, etc).
func contentToString(v interface{}) string {
	switch v := v.(type) {
	case string:
		return v
	case []interface{}:
		var s string
		for _, item := range v {
			if m, ok := item.(map[string]interface{}); ok {
				if t := getString(m, "type"); t == "text" {
					s += getString(m, "text")
				}
			}
		}
		return s
	default:
		b, _ := marshalJSON(v)
		return string(b)
	}
}

// parseJSONString parses a JSON string into a target.
func parseJSONString(s string, target interface{}) error {
	return sonic.Unmarshal([]byte(s), target)
}

// marshalJSON marshals to JSON bytes.
func marshalJSON(v interface{}) ([]byte, error) {
	return sonic.Marshal(v)
}

// now returns current Unix timestamp.
func now() int64 {
	return time.Now().Unix()
}

// maybeNow returns ts if > 0, otherwise current time.
func maybeNow(ts int64) int64 {
	if ts > 0 {
		return ts
	}
	return now()
}

// compactJSON returns compact JSON string for logging.
func compactJSON(body []byte) string {
	if body == nil {
		return ""
	}
	var v interface{}
	if err := sonic.Unmarshal(body, &v); err != nil {
		return string(body)
	}
	b, err := sonic.Marshal(v)
	if err != nil {
		return string(body)
	}
	return string(b)
}

// getStringFromMap safely gets string from nested map.
func getStringFromMap(m map[string]interface{}, keys ...string) string {
	current := m
	for i, key := range keys {
		if i == len(keys)-1 {
			return getString(current, key)
		}
		if v, ok := current[key]; ok {
			if next, ok := v.(map[string]interface{}); ok {
				current = next
			} else {
				return ""
			}
		} else {
			return ""
		}
	}
	return ""
}
