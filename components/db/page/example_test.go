package page_test

import (
	"fmt"

	"github.com/apus-run/gala/components/db/page"
)

// ExampleNewMeta 展示页码参数归一与元数据计算。
func ExampleNewMeta() {
	params := page.Params{Page: 2, Size: 10}
	fmt.Println(params.Offset(), params.Limit())

	meta := page.NewMeta(params, 25)
	fmt.Printf("page=%d per_page=%d total_pages=%d has_next=%v has_prev=%v\n",
		meta.Page, meta.PerPage, meta.TotalPages, meta.HasNext, meta.HasPrev)

	// Output:
	// 10 10
	// page=2 per_page=10 total_pages=3 has_next=true has_prev=true
}

// ExampleParams_zeroValue 展示零值也能直接用，不需要先归一。
func ExampleParams_zeroValue() {
	var params page.Params
	fmt.Println(params.Offset(), params.Limit())

	meta := page.NewMeta(params, 0)
	fmt.Printf("page=%d per_page=%d total_pages=%d has_next=%v\n",
		meta.Page, meta.PerPage, meta.TotalPages, meta.HasNext)

	// Output:
	// 0 20
	// page=1 per_page=20 total_pages=0 has_next=false
}
