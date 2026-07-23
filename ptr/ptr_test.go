package ptr

import "testing"

func TestRef(t *testing.T) {
	tests := []struct {
		name string
		val  any
	}{
		{"int", 42},
		{"string", "hello"},
		{"bool", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			switch v := tt.val.(type) {
			case int:
				p := Ref(v)
				if p == nil || *p != v {
					t.Errorf("Ref(%v) = %v, want pointer to %v", v, p, v)
				}
			case string:
				p := Ref(v)
				if p == nil || *p != v {
					t.Errorf("Ref(%v) = %v, want pointer to %v", v, p, v)
				}
			case bool:
				p := Ref(v)
				if p == nil || *p != v {
					t.Errorf("Ref(%v) = %v, want pointer to %v", v, p, v)
				}
			}
		})
	}
}

func TestDeref(t *testing.T) {
	val := 42
	if got := Deref(&val); got != 42 {
		t.Errorf("Deref(&42) = %d, want 42", got)
	}
	if got := Deref[int](nil); got != 0 {
		t.Errorf("Deref(nil) = %d, want 0", got)
	}

	s := "hello"
	if got := Deref(&s); got != "hello" {
		t.Errorf("Deref(&\"hello\") = %q, want \"hello\"", got)
	}
	if got := Deref[string](nil); got != "" {
		t.Errorf("Deref[string](nil) = %q, want empty string", got)
	}
}
