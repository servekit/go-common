package configx

import (
	"reflect"
	"strings"
	"unicode"
)

// collectDefaults walks target (expected to be a non-nil pointer to a struct)
// and returns a map of dotted snake_case keys to default tag string values.
// Returns an empty map if target is not a struct pointer; the subsequent
// viper.Unmarshal will surface that case as ErrUnmarshal.
//
// Walking is type-based, not value-based: nil pointer-to-struct fields are
// still walked (defaults are a static property of the type, not the value).
//
// Walk rules (see "Default Values from Tags" in the package doc):
//   - Nested structs are walked recursively; key paths are dotted.
//   - Embedded (anonymous) fields are walked transparently — the embedded
//     type name is NOT added to the key path.
//   - Tags on struct or pointer-to-struct fields are ignored (no scalar
//     default form); walk into them to collect inner-field defaults.
//   - Tags on primitive or pointer-to-primitive fields are recorded.
//   - Unexported fields are skipped.
//   - An empty `default:""` tag is treated as not set.
//   - Cyclic struct types (e.g. linked-list Node{Next *Node}) are guarded:
//     a struct type appearing in its own ancestor chain is skipped, so a
//     self-referential type cannot cause infinite recursion. Sibling subtrees
//     of the same type (e.g. Primary/Secondary of the same DBConfig) each
//     contribute their own defaults.
func collectDefaults(target any) map[string]any {
	out := make(map[string]any)
	// Top-level value check: a typed nil pointer (e.g. (*Cfg)(nil)) has a
	// valid type but no value to unmarshal into — treat it as "not a struct
	// pointer" per the docstring's "non-nil" contract. Nested nil
	// pointer-to-struct fields are still walked (see walkStruct) because
	// defaults are a static property of the type.
	v := reflect.ValueOf(target)
	t := reflect.TypeOf(target)
	for t != nil && t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t == nil || t.Kind() != reflect.Struct {
		return out
	}
	if v.Kind() == reflect.Pointer && v.IsNil() {
		return out
	}
	walkStruct(t, "", out, make(map[reflect.Type]bool))
	return out
}

// walkStruct recurses through a struct type, recording default-tagged fields
// into out. prefix is the dotted key path accumulated so far.
//
// ancestors is an ancestor-chain set: a struct type is marked while its subtree
// is on the recursion stack, then removed on return. This breaks true cycles
// (a struct type appearing in its own ancestor chain — e.g. linked-list
// Node{Next *Node}) while still allowing sibling subtrees of the same type
// (e.g. Primary/Secondary of the same DBConfig) to contribute their own
// defaults.
func walkStruct(t reflect.Type, prefix string, out map[string]any, ancestors map[reflect.Type]bool) {
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		if !field.IsExported() {
			continue
		}

		// Compute this field's key prefix.
		fieldKey := prefix
		if !field.Anonymous {
			if fieldKey != "" {
				fieldKey += "."
			}
			fieldKey += toSnakeCase(field.Name)
		}

		// Underlying type (deref pointer in the type domain).
		ft := field.Type
		underlying := ft
		if underlying.Kind() == reflect.Pointer {
			underlying = underlying.Elem()
		}

		// Record tag for non-anonymous, non-struct fields. Tags on struct
		// or pointer-to-struct fields don't have a scalar meaning — those
		// fields are containers, and defaults belong on their inner fields.
		if !field.Anonymous && underlying.Kind() != reflect.Struct {
			if def, ok := field.Tag.Lookup("default"); ok && def != "" {
				out[fieldKey] = def
			}
		}

		// Recurse into struct-like fields (struct or pointer-to-struct).
		// Nil pointer-to-struct is walked transparently via type domain.
		// ancestors is an ancestor-chain set: a type is marked while its subtree
		// is on the recursion stack, then removed on return. This breaks true
		// cycles (a type appearing in its own ancestor chain) while still
		// allowing sibling subtrees of the same type to contribute their own
		// defaults.
		if underlying.Kind() == reflect.Struct {
			if ancestors[underlying] {
				continue
			}
			ancestors[underlying] = true
			walkStruct(underlying, fieldKey, out, ancestors)
			delete(ancestors, underlying)
		}
	}
}

// toSnakeCase converts CamelCase to snake_case.
//
//	GRPCAddr     → grpc_addr
//	HTTPServer   → http_server
//	OAuth        → oauth
//	OAuth2       → oauth2
//	Simple       → simple
//
// Algorithm:
//   - Insert '_' between [lowercase/digit] and uppercase: simpleField → simple_field.
//   - Insert '_' between consecutive uppercase letters when the last one
//     starts a new word (current is upper, next is lower, previous two
//     chars are upper): HTTPServer → http_server.
//   - Digits attach to the preceding word.
//   - Output is all lowercase.
func toSnakeCase(s string) string {
	if s == "" {
		return ""
	}
	runes := []rune(s)
	var sb strings.Builder
	for i, r := range runes {
		if i > 0 && unicode.IsUpper(r) {
			prev := runes[i-1]
			if unicode.IsLower(prev) || unicode.IsDigit(prev) {
				sb.WriteRune('_')
			} else if i+1 < len(runes) && unicode.IsLower(runes[i+1]) &&
				i >= 2 && unicode.IsUpper(runes[i-2]) {
				sb.WriteRune('_')
			}
		}
		sb.WriteRune(unicode.ToLower(r))
	}
	return sb.String()
}
