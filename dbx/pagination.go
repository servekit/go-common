package dbx

import "gorm.io/gorm"

// This file provides two pagination strategies:
//
//  1. Offset pagination (OffsetPaginate) — for admin panels, dashboards, and
//     any UI that needs "page X of Y" with total counts. Simpler but slower on
//     large offsets; runs COUNT(*) by default.
//
//  2. Keyset/cursor pagination (Pagination + TrimPage) — for infinite scroll,
//     feed-style lists, and high-churn tables. No COUNT(*) needed, stable
//     results under concurrent writes, consistent O(1) performance.

// PageParams controls offset-based pagination behavior.
type PageParams struct {
	Page     int  // 1-based page number
	PageSize int  // clamped to [DefaultPageSize, MaxPageSize] by Normalize
	Count    bool // whether to run COUNT(*); set false to skip total calculation
}

// PageResult holds paginated query results from OffsetPaginate.
type PageResult[T any] struct {
	List       []T
	Total      int64 // 0 when PageParams.Count is false
	TotalPages int   // 0 when PageParams.Count is false
}

// Pagination holds keyset-based pagination parameters.
type Pagination struct {
	PageSize int   // clamped to [DefaultPageSize, MaxPageSize] by Normalize
	AfterID  int64 // keyset cursor: return items with id < AfterID; 0 = first page
}

const (
	// DefaultPageSize is the page size used when PageSize <= 0.
	DefaultPageSize = 20
	// MaxPageSize is the upper bound on PageSize after clamping.
	MaxPageSize = 100
)

// Normalize clamps Page and PageSize to valid ranges.
func (p PageParams) Normalize() PageParams {
	if p.Page < 1 {
		p.Page = 1
	}
	p.PageSize = ClampPageSize(p.PageSize)
	return p
}

// Normalize returns a copy with PageSize clamped and AfterID unchanged.
func (p Pagination) Normalize() Pagination {
	p.PageSize = ClampPageSize(p.PageSize)
	return p
}

// FetchLimit returns the Limit value for queries: PageSize + 1 to detect next page.
func (p Pagination) FetchLimit() int {
	return p.PageSize + 1
}

// OffsetPaginate runs offset-based pagination on tx.
// Pre-apply all WHERE conditions to tx before calling.
func OffsetPaginate[T any](tx *gorm.DB, p PageParams) (*PageResult[T], error) {
	p = p.Normalize()

	var total int64
	if p.Count {
		if err := tx.Session(&gorm.Session{}).Count(&total).Error; err != nil {
			return nil, err
		}
	}

	var list []T
	if !p.Count || total > 0 {
		if err := tx.Session(&gorm.Session{}).
			Offset((p.Page - 1) * p.PageSize).
			Limit(p.PageSize).
			Find(&list).Error; err != nil {
			return nil, err
		}
	}

	var totalPages int
	if p.Count {
		totalPages = int((total + int64(p.PageSize) - 1) / int64(p.PageSize))
	}

	return &PageResult[T]{
		List:       list,
		Total:      total,
		TotalPages: totalPages,
	}, nil
}

// ClampPageSize returns size clamped to [DefaultPageSize, MaxPageSize].
func ClampPageSize(size int) int {
	if size <= 0 {
		return DefaultPageSize
	}
	if size > MaxPageSize {
		return MaxPageSize
	}
	return size
}

// TrimPage trims items to pageSize and reports whether a next page exists.
// items must have been fetched with FetchLimit (pageSize+1).
func TrimPage[T any](items []T, pageSize int) (trimmed []T, hasNext bool) {
	if len(items) > pageSize {
		return items[:pageSize], true
	}
	return items, false
}
