package jsonx

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type user struct {
	Name  string   `json:"name"`
	Age   int      `json:"age"`
	Email string   `json:"email,omitempty"`
	Tags  []string `json:"tags,omitempty"`
}

func TestMarshal_Unmarshal(t *testing.T) {
	u := user{Name: "Alice", Age: 30, Tags: []string{"go", "redis"}}

	data, err := Marshal(u)
	require.NoError(t, err)

	var got user
	err = Unmarshal(data, &got)
	require.NoError(t, err)
	assert.Equal(t, u, got)
}

func TestMarshalString_UnmarshalString(t *testing.T) {
	u := user{Name: "Bob", Age: 25}

	s, err := MarshalString(u)
	require.NoError(t, err)
	assert.Contains(t, s, `"name":"Bob"`)

	var got user
	err = UnmarshalString(s, &got)
	require.NoError(t, err)
	assert.Equal(t, u, got)
}

func TestMarshal_Omitempty(t *testing.T) {
	u := user{Name: "Charlie", Age: 20}

	data, err := Marshal(u)
	require.NoError(t, err)
	assert.NotContains(t, string(data), `"email"`)
	assert.NotContains(t, string(data), `"tags"`)
}

func TestMarshalIndent(t *testing.T) {
	u := user{Name: "Dave", Age: 40}
	data, err := MarshalIndent(u, "", "  ")
	require.NoError(t, err)
	assert.Contains(t, string(data), "  \"name\": \"Dave\"")
}

func TestValid(t *testing.T) {
	assert.True(t, Valid([]byte(`{"key": "value"}`)))
	assert.True(t, Valid([]byte(`[1, 2, 3]`)))
	assert.False(t, Valid([]byte(`{invalid}`)))
	assert.False(t, Valid([]byte(``)))
}

func TestEncode_Decode(t *testing.T) {
	u := user{Name: "Eve", Age: 28, Email: "eve@test.com"}

	var buf bytes.Buffer
	err := Encode(&buf, u)
	require.NoError(t, err)

	var got user
	err = Decode(&buf, &got)
	require.NoError(t, err)
	assert.Equal(t, u, got)
}

func TestUnmarshal_InvalidJSON(t *testing.T) {
	var got user
	err := Unmarshal([]byte(`{bad json}`), &got)
	assert.Error(t, err)
}

func TestUnmarshal_StringInput(t *testing.T) {
	input := `{"name":"Frank","age":35}`
	var got user
	err := UnmarshalString(input, &got)
	require.NoError(t, err)
	assert.Equal(t, "Frank", got.Name)
	assert.Equal(t, 35, got.Age)
}

func BenchmarkMarshal(b *testing.B) {
	u := user{Name: "Alice", Age: 30, Email: "alice@test.com", Tags: []string{"go", "redis"}}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = Marshal(u)
	}
}

func BenchmarkUnmarshal(b *testing.B) {
	data := []byte(`{"name":"Alice","age":30,"email":"alice@test.com","tags":["go","redis"]}`)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var u user
		_ = Unmarshal(data, &u)
	}
}

func BenchmarkMarshalString(b *testing.B) {
	u := user{Name: "Alice", Age: 30, Email: "alice@test.com", Tags: []string{"go", "redis"}}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = MarshalString(u)
	}
}

func BenchmarkUnmarshalString(b *testing.B) {
	data := `{"name":"Alice","age":30,"email":"alice@test.com","tags":["go","redis"]}`
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var u user
		_ = UnmarshalString(data, &u)
	}
}

// Compare with encoding/json to demonstrate the speedup.
func BenchmarkStdMarshal(b *testing.B) {
	u := user{Name: "Alice", Age: 30, Email: "alice@test.com", Tags: []string{"go", "redis"}}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = json.Marshal(u)
	}
}

func BenchmarkStdUnmarshal(b *testing.B) {
	data := []byte(`{"name":"Alice","age":30,"email":"alice@test.com","tags":["go","redis"]}`)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var u user
		_ = json.Unmarshal(data, &u)
	}
}
