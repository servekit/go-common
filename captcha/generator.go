package captcha

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"strings"
)

// CodeFormat configures the format of a generated verification code.
type CodeFormat struct {
	Length  int    // code length, defaults to 6
	Charset string // digit / alpha / alphanumeric
	Case    string // upper / lower / mixed (only applies to alpha and alphanumeric)
}

// CodeGenerator generates verification codes with different formats per purpose.
type CodeGenerator struct {
	formats map[string]*CodeFormat
}

// NewCodeGenerator creates a code generator. Formats maps purpose to format.
func NewCodeGenerator(formats map[string]*CodeFormat) *CodeGenerator {
	return &CodeGenerator{formats: formats}
}

// Predefined formats and internal charsets.
var (
	FormatDigit6      = &CodeFormat{Length: 6, Charset: "digit"}
	FormatDigit4      = &CodeFormat{Length: 4, Charset: "digit"}
	FormatAlphaNum8   = &CodeFormat{Length: 8, Charset: "alphanumeric", Case: "upper"}
	FormatAlphaMixed6 = &CodeFormat{Length: 6, Charset: "alpha", Case: "mixed"}

	digits        = "0123456789"
	alphaLower    = "abcdefghijklmnopqrstuvwxyz"
	alphaUpper    = "ABCDEFGHIJKLMNOPQRSTUVWXYZ"
	alphaMixed    = alphaLower + alphaUpper
	alphanumeric  = digits + alphaUpper
	alphanumMixed = digits + alphaMixed
)

// Generate produces a verification code for the given purpose.
func (g *CodeGenerator) Generate(purpose string) (string, error) {
	f := FormatDigit6 // default: 6-digit number
	if found, ok := g.formats[purpose]; ok {
		f = found
	}

	charset := g.charset(f)
	length := f.Length
	if length <= 0 {
		length = 6
	}

	var buf strings.Builder
	for i := 0; i < length; i++ {
		idx, err := rand.Int(rand.Reader, big.NewInt(int64(len(charset))))
		if err != nil {
			return "", fmt.Errorf("generate code: %w", err)
		}
		buf.WriteByte(charset[idx.Int64()])
	}
	return buf.String(), nil
}

func (g *CodeGenerator) charset(f *CodeFormat) string { //nolint:revive // pure function on value
	switch f.Charset {
	case "digit":
		return digits
	case "alpha":
		switch f.Case {
		case "upper":
			return alphaUpper
		case "lower":
			return alphaLower
		default:
			return alphaMixed
		}
	case "alphanumeric":
		switch f.Case {
		case "lower":
			return digits + alphaLower
		case "mixed":
			return alphanumMixed
		default:
			return alphanumeric
		}
	default:
		return digits
	}
}
