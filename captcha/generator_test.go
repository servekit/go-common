package captcha

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCodeGenerator_Digit6(t *testing.T) {
	gen := NewCodeGenerator(map[string]*CodeFormat{
		"register": FormatDigit6,
	})
	code, err := gen.Generate("register")
	require.NoError(t, err)
	require.Len(t, code, 6)
	for _, c := range code {
		require.True(t, c >= '0' && c <= '9', "expected digit, got %c", c)
	}
}

func TestCodeGenerator_Digit4(t *testing.T) {
	gen := NewCodeGenerator(map[string]*CodeFormat{
		"bind": FormatDigit4,
	})
	code, err := gen.Generate("bind")
	require.NoError(t, err)
	require.Len(t, code, 4)
}

func TestCodeGenerator_AlphaNum8(t *testing.T) {
	gen := NewCodeGenerator(map[string]*CodeFormat{
		"test": FormatAlphaNum8,
	})
	code, err := gen.Generate("test")
	require.NoError(t, err)
	require.Len(t, code, 8)
}

func TestCodeGenerator_DefaultFormat(t *testing.T) {
	gen := NewCodeGenerator(nil)
	code, err := gen.Generate("unknown_purpose")
	require.NoError(t, err)
	require.Len(t, code, 6) // default 6 digits
}

func TestCodeGenerator_Uniqueness(t *testing.T) {
	gen := NewCodeGenerator(map[string]*CodeFormat{
		"test": FormatDigit6,
	})
	codes := make(map[string]bool)
	for i := 0; i < 100; i++ {
		code, err := gen.Generate("test")
		require.NoError(t, err)
		codes[code] = true
	}
	// 100 codes should have a high uniqueness rate (allow some collisions).
	require.Greater(t, len(codes), 90)
}

func TestCodeGenerator_AlphaMixed(t *testing.T) {
	gen := NewCodeGenerator(map[string]*CodeFormat{
		"test": FormatAlphaMixed6,
	})
	code, err := gen.Generate("test")
	require.NoError(t, err)
	require.Len(t, code, 6)
	hasLetter := false
	for _, c := range code {
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') {
			hasLetter = true
			break
		}
	}
	require.True(t, hasLetter, "expected at least one letter in mixed alpha code")
}

func TestCodeGenerator_AlphaLower(t *testing.T) {
	gen := NewCodeGenerator(map[string]*CodeFormat{
		"test": {Length: 6, Charset: "alpha", Case: "lower"},
	})
	code, err := gen.Generate("test")
	require.NoError(t, err)
	require.Len(t, code, 6)
	for _, c := range code {
		require.True(t, c >= 'a' && c <= 'z', "expected lowercase letter, got %c", c)
	}
}

func TestCodeGenerator_AlphaUpper(t *testing.T) {
	gen := NewCodeGenerator(map[string]*CodeFormat{
		"test": {Length: 6, Charset: "alpha", Case: "upper"},
	})
	code, err := gen.Generate("test")
	require.NoError(t, err)
	for _, c := range code {
		require.True(t, c >= 'A' && c <= 'Z', "expected uppercase letter, got %c", c)
	}
}

func TestCodeGenerator_AlphanumLower(t *testing.T) {
	gen := NewCodeGenerator(map[string]*CodeFormat{
		"test": {Length: 10, Charset: "alphanumeric", Case: "lower"},
	})
	code, err := gen.Generate("test")
	require.NoError(t, err)
	require.Len(t, code, 10)
	for _, c := range code {
		require.True(t, (c >= '0' && c <= '9') || (c >= 'a' && c <= 'z'),
			"expected digit or lowercase, got %c", c)
	}
}

func TestCodeGenerator_AlphanumMixed(t *testing.T) {
	gen := NewCodeGenerator(map[string]*CodeFormat{
		"test": {Length: 10, Charset: "alphanumeric", Case: "mixed"},
	})
	code, err := gen.Generate("test")
	require.NoError(t, err)
	require.Len(t, code, 10)
}

func TestCodeGenerator_DefaultCharset(t *testing.T) {
	gen := NewCodeGenerator(map[string]*CodeFormat{
		"test": {Length: 4, Charset: "unknown"},
	})
	code, err := gen.Generate("test")
	require.NoError(t, err)
	require.Len(t, code, 4)
	for _, c := range code {
		require.True(t, c >= '0' && c <= '9', "expected digit for unknown charset, got %c", c)
	}
}
