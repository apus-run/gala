// Command examples demonstrates the full use of pagination and sorting.
//
//	go run ./examples
//
// It covers four things: whitelist-based sort parsing, page parameter
// normalization, reusing one Options for both the count query and the page
// query, and rejecting sort fields outside the whitelist. See
// examples/userlist for a version closer to real business code.
// Command examples 演示分页与排序的完整用法。
//
//	go run ./examples
//
// 覆盖四件事：白名单排序解析、分页参数归一、同一份 Options 用于计数与查询、
// 非法排序字段被拒绝。更接近真实业务的版本见 examples/userlist。
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/url"

	"gorm.io/driver/sqlite"

	"github.com/apus-run/gala/components/db"
	"github.com/apus-run/gala/components/db/page"
	"github.com/apus-run/gala/components/db/store"
	"github.com/apus-run/gala/components/db/where"
)

// User is the example model | User 是示例模型。
type User struct {
	ID        uint   `gorm:"primaryKey" json:"id"`
	Name      string `gorm:"size:64" json:"name"`
	CreatedAt int64  `json:"created_at"`
}

func main() {
	ctx := context.Background()

	provider, err := db.NewDB(sqlite.Open("file:example?mode=memory&cache=shared"))
	if err != nil {
		log.Fatal(err)
	}
	defer provider.Close()

	if err = provider.DB(ctx).AutoMigrate(&User{}); err != nil {
		log.Fatal(err)
	}
	for i, name := range []string{"alice", "bob", "carol", "dave", "erin"} {
		if err = provider.DB(ctx).Create(&User{Name: name, CreatedAt: int64(i)}).Error; err != nil {
			log.Fatal(err)
		}
	}

	// 1. Whitelist: the sort from a request can only land on these two columns.
	// 1. 白名单：请求里的 sort 只可能落到这两列上。
	sortable := where.Sortable{
		"name":       "name",
		"created_at": "created_at",
	}
	columns, err := sortable.Parse("-created_at,name")
	if err != nil {
		log.Fatal(err) // map to a 400 in a real service | 真实服务里映射成 400
	}

	// 2. Pagination + sorting: hand the same Options to the store; it is applied
	// once for the count and once for the query.
	// 2. 分页 + 排序：同一个 Options 交给 store，计数与查询各应用一次。
	params, err := page.ParsePageParams(url.Values{"page": {"2"}, "size": {"2"}})
	if err != nil {
		log.Fatal(err) // map ErrInvalidPagination to HTTP 400 | 将 ErrInvalidPagination 映射为 HTTP 400
	}
	pagination, err := page.FromPage(params.Page, params.Size)
	if err != nil {
		log.Fatal(err) // reject an overflowing offset | 拒绝偏移溢出
	}
	// Offset/limit endpoints use ParsePagination directly.
	// offset/limit 接口直接使用 ParsePagination。
	userStore := store.NewStore[User](provider)
	opts := where.O(pagination.Offset).L(pagination.Limit).S(columns...)
	total, users, err := userStore.List(ctx, opts)
	if err != nil {
		log.Fatal(err)
	}

	// 3. Both inputs share a response with the actual query offset and limit.
	// 3. 两种输入共用响应结构，返回实际查询使用的偏移和条数。
	body := page.NewPage(users, total, pagination)
	encoded, err := json.MarshalIndent(body, "", "  ")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(string(encoded))

	// 4. Fields outside the whitelist are rejected outright, never spliced into SQL.
	// 4. 白名单外的字段直接拒绝，不会拼进 SQL。
	if _, err = sortable.Parse("name;drop table users"); err != nil {
		fmt.Println("rejected:", err)
	}
}
