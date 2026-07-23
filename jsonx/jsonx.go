// Package jsonx provides high-performance JSON operations powered by sonic.
// It is a drop-in replacement for encoding/json in performance-sensitive code.
//
// On unsupported platforms, sonic automatically falls back to encoding/json.
package jsonx

import (
	"io"

	"github.com/bytedance/sonic"
)

// Marshal returns the JSON encoding of v.
func Marshal(v any) ([]byte, error) {
	return sonic.Marshal(v)
}

// Unmarshal parses the JSON-encoded data and stores the result in v.
func Unmarshal(data []byte, v any) error {
	return sonic.Unmarshal(data, v)
}

// MarshalString is like Marshal but returns a string, avoiding the []byte → string copy.
func MarshalString(v any) (string, error) {
	return sonic.MarshalString(v)
}

// UnmarshalString is like Unmarshal but accepts a string, avoiding the string → []byte copy.
func UnmarshalString(data string, v any) error {
	return sonic.UnmarshalString(data, v)
}

// MarshalIndent is like Marshal but applies Indent to format the output.
func MarshalIndent(v any, prefix, indent string) ([]byte, error) {
	return sonic.MarshalIndent(v, prefix, indent)
}

// Valid reports whether data is a valid JSON encoding.
func Valid(data []byte) bool {
	return sonic.Valid(data)
}

// Decode reads the next JSON-encoded value from r and stores it in v.
func Decode(r io.Reader, v any) error {
	return sonic.ConfigDefault.NewDecoder(r).Decode(v)
}

// Encode writes the JSON encoding of v to w.
func Encode(w io.Writer, v any) error {
	return sonic.ConfigDefault.NewEncoder(w).Encode(v)
}
