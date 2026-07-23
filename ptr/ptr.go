// Package ptr provides generic pointer utility functions.
package ptr

// Ref returns a pointer to the given value.
func Ref[T any](v T) *T { return &v }

// Deref dereferences a pointer, returning the zero value if nil.
func Deref[T any](p *T) T {
	if p == nil {
		var zero T
		return zero
	}
	return *p
}
