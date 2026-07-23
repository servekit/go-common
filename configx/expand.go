package configx

import "os"

// expandStrings walks m recursively and applies os.ExpandEnv to every string
// value. Nested map[string]any and []any are walked; non-string values are
// left untouched.
//
// unset variables expand to empty string (os.ExpandEnv semantics).
func expandStrings(m map[string]any) {
	for k, v := range m {
		m[k] = expandValue(v)
	}
}

func expandValue(v any) any {
	switch val := v.(type) {
	case string:
		return os.ExpandEnv(val)
	case map[string]any:
		expandStrings(val)
		return val
	case []any:
		for i, item := range val {
			val[i] = expandValue(item)
		}
		return val
	default:
		return v
	}
}
