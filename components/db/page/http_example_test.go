package page_test

import (
	"errors"
	"fmt"
	"net/url"

	"github.com/apus-run/gala/components/db/page"
)

func ExampleParsePagination() {
	// In a handler, pass r.URL.Query() (or parsed query values).
	// Handler 中传入 r.URL.Query() 或已解析的查询参数。
	values := url.Values{"offset": {"15"}, "limit": {"10"}}
	p, err := page.ParsePagination(values)
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(p.Offset, p.Limit)

	// Output: 15 10
}

func ExampleParsePageParams() {
	params, err := page.ParsePageParams(url.Values{"page": {"3"}, "size": {"20"}})
	if err != nil {
		fmt.Println(err)
		return
	}
	p, err := page.FromPage(params.Page, params.Size)
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(p.Offset, p.Limit)

	// Output: 40 20
}

func ExampleParsePagination_invalid() {
	_, err := page.ParsePagination(url.Values{"limit": {"101"}})
	if errors.Is(err, page.ErrInvalidPagination) {
		fmt.Println("return HTTP 400")
	}

	// Output: return HTTP 400
}
