package pagination

import "testing"

func TestNormalizeSortOrder(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		input        string
		defaultOrder string
		want         string
	}{
		{name: "asc", input: "asc", defaultOrder: "desc", want: "asc"},
		{name: "uppercase asc", input: "ASC", defaultOrder: "desc", want: "asc"},
		{name: "desc", input: "desc", defaultOrder: "asc", want: "desc"},
		{name: "trim spaces", input: "  desc  ", defaultOrder: "asc", want: "desc"},
		{name: "invalid falls back", input: "sideways", defaultOrder: "asc", want: "asc"},
		{name: "empty falls back", input: "", defaultOrder: "desc", want: "desc"},
		{name: "invalid default falls back to desc", input: "", defaultOrder: "wat", want: "desc"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := NormalizeSortOrder(tt.input, tt.defaultOrder); got != tt.want {
				t.Fatalf("NormalizeSortOrder(%q, %q) = %q, want %q", tt.input, tt.defaultOrder, got, tt.want)
			}
		})
	}
}

func TestPaginationParamsNormalizedSortOrder(t *testing.T) {
	t.Parallel()

	params := PaginationParams{SortOrder: "ASC"}
	if got := params.NormalizedSortOrder("desc"); got != "asc" {
		t.Fatalf("NormalizedSortOrder = %q, want asc", got)
	}

	params = PaginationParams{SortOrder: "bad"}
	if got := params.NormalizedSortOrder("asc"); got != "asc" {
		t.Fatalf("NormalizedSortOrder invalid fallback = %q, want asc", got)
	}
}

func TestPaginationParamsLimit(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		pageSize int
		want     int
	}{
		{name: "non-positive falls back to default", pageSize: 0, want: 20},
		{name: "negative falls back to default", pageSize: -1, want: 20},
		{name: "normal value keeps", pageSize: 50, want: 50},
		{name: "max value keeps", pageSize: 1000, want: 1000},
		{name: "beyond max clamps to 1000", pageSize: 1500, want: 1000},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			p := PaginationParams{PageSize: tt.pageSize}
			if got := p.Limit(); got != tt.want {
				t.Fatalf("Limit() for PageSize=%d = %d, want %d", tt.pageSize, got, tt.want)
			}
		})
	}
}

func TestPaginationParamsOffsetUsesNormalizedLimit(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		page     int
		pageSize int
		want     int
	}{
		{name: "invalid page uses first page", page: 0, pageSize: 50, want: 0},
		{name: "zero page size uses default", page: 2, pageSize: 0, want: 20},
		{name: "negative page size uses default", page: 2, pageSize: -1, want: 20},
		{name: "normal values", page: 3, pageSize: 50, want: 100},
		{name: "page size beyond max is clamped", page: 2, pageSize: 1500, want: 1000},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			params := PaginationParams{Page: tt.page, PageSize: tt.pageSize}
			if got := params.Offset(); got != tt.want {
				t.Fatalf("Offset() for Page=%d, PageSize=%d = %d, want %d", tt.page, tt.pageSize, got, tt.want)
			}
		})
	}
}

func TestPaginationParamsNormalizedPageCapsPage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		page int
		want int
	}{
		{name: "zero falls back to 1", page: 0, want: 1},
		{name: "negative falls back to 1", page: -5, want: 1},
		{name: "normal value keeps", page: 42, want: 42},
		{name: "max value keeps", page: MaxPage, want: MaxPage},
		{name: "beyond max clamps", page: MaxPage + 1, want: MaxPage},
		{name: "huge value clamps", page: 1 << 40, want: MaxPage},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			p := PaginationParams{Page: tt.page}
			if got := p.NormalizedPage(); got != tt.want {
				t.Fatalf("NormalizedPage() for Page=%d = %d, want %d", tt.page, got, tt.want)
			}
		})
	}
}

func TestPaginationParamsOffsetIsBounded(t *testing.T) {
	t.Parallel()

	// 页码与每页条数都超出上限时，Offset 不得超过 (MaxPage-1)*MaxPageSize。
	p := PaginationParams{Page: 1 << 40, PageSize: 1 << 30}
	if got, want := p.Offset(), (MaxPage-1)*MaxPageSize; got != want {
		t.Fatalf("Offset() = %d, want %d", got, want)
	}
	if got := p.Limit(); got != MaxPageSize {
		t.Fatalf("Limit() = %d, want %d", got, MaxPageSize)
	}
}

func TestPaginationParamsNormalized(t *testing.T) {
	t.Parallel()

	p := PaginationParams{Page: 0, PageSize: 0, SortBy: "id", SortOrder: "asc"}
	got := p.Normalized()
	if got.Page != 1 || got.PageSize != DefaultPageSize {
		t.Fatalf("Normalized() defaults = page %d size %d, want 1/%d", got.Page, got.PageSize, DefaultPageSize)
	}
	if got.SortBy != "id" || got.SortOrder != "asc" {
		t.Fatalf("Normalized() must preserve sort fields, got %+v", got)
	}

	p = PaginationParams{Page: MaxPage + 7, PageSize: MaxPageSize + 7}
	got = p.Normalized()
	if got.Page != MaxPage || got.PageSize != MaxPageSize {
		t.Fatalf("Normalized() caps = page %d size %d, want %d/%d", got.Page, got.PageSize, MaxPage, MaxPageSize)
	}
	// 原值不被修改（值接收者）。
	if p.Page != MaxPage+7 {
		t.Fatalf("Normalized() must not mutate receiver, got Page=%d", p.Page)
	}
}
