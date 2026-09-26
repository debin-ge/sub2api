// Package pagination provides types and helpers for paginated responses.
package pagination

import "strings"

const (
	SortOrderAsc  = "asc"
	SortOrderDesc = "desc"

	// DefaultPageSize 未指定或非法 page_size 时的默认每页条数。
	DefaultPageSize = 20
	// MaxPageSize 单页条数上限：超出会被截断，避免一次查询拖出整张表。
	MaxPageSize = 1000
	// MaxPage 页码上限：超出会被截断，避免 OFFSET 被推到天文数字触发全表扫描。
	MaxPage = 10000
)

// PaginationParams 分页参数
type PaginationParams struct {
	Page      int
	PageSize  int
	SortBy    string
	SortOrder string
}

// PaginationResult 分页结果
type PaginationResult struct {
	Total    int64
	Page     int
	PageSize int
	Pages    int
}

// DefaultPagination 默认分页参数
func DefaultPagination() PaginationParams {
	return PaginationParams{
		Page:      1,
		PageSize:  20,
		SortOrder: SortOrderDesc,
	}
}

// NormalizedPage 返回夹在 [1, MaxPage] 内的页码。
func (p PaginationParams) NormalizedPage() int {
	if p.Page < 1 {
		return 1
	}
	if p.Page > MaxPage {
		return MaxPage
	}
	return p.Page
}

// Offset 计算偏移量（页码与每页条数均先做上下限归一）。
func (p PaginationParams) Offset() int {
	return (p.NormalizedPage() - 1) * p.Limit()
}

// Limit 获取限制数：非法值回落到 DefaultPageSize，超出 MaxPageSize 截断。
func (p PaginationParams) Limit() int {
	if p.PageSize < 1 {
		return DefaultPageSize
	}
	if p.PageSize > MaxPageSize {
		return MaxPageSize
	}
	return p.PageSize
}

// Normalized 返回 Page/PageSize 已归一到合法区间的副本，供需要回显分页参数的调用方使用。
func (p PaginationParams) Normalized() PaginationParams {
	p.Page = p.NormalizedPage()
	p.PageSize = p.Limit()
	return p
}

// NormalizeSortOrder normalizes sort order to asc/desc and falls back to defaultOrder.
func NormalizeSortOrder(order string, defaultOrder string) string {
	switch strings.ToLower(strings.TrimSpace(defaultOrder)) {
	case SortOrderAsc:
		defaultOrder = SortOrderAsc
	default:
		defaultOrder = SortOrderDesc
	}

	switch strings.ToLower(strings.TrimSpace(order)) {
	case SortOrderAsc:
		return SortOrderAsc
	case SortOrderDesc:
		return SortOrderDesc
	default:
		return defaultOrder
	}
}

// NormalizedSortOrder returns the normalized sort order using defaultOrder as fallback.
func (p PaginationParams) NormalizedSortOrder(defaultOrder string) string {
	return NormalizeSortOrder(p.SortOrder, defaultOrder)
}
