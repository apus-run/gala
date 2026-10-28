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

	"gorm.io/driver/sqlite"

	"github.com/apus-run/gala/components/db"
	"github.com/apus-run/gala/components/db/page"
	"github.com/apus-run/gala/components/db/store"
	"github.com/apus-run/gala/components/db/where"
)

// User 是示例模型。
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

	// 1. 白名单：请求里的 sort 只可能落到这两列上。
	sortable := where.Sortable{
		"name":       "name",
		"created_at": "created_at",
	}
	columns, err := sortable.Parse("-created_at,name")
	if err != nil {
		log.Fatal(err) // 真实服务里映射成 400
	}

	// 2. 分页 + 排序：同一个 Options 交给 store，计数与查询各应用一次。
	params := page.Params{Page: 2, Size: 2}
	userStore := store.NewStore[User](provider)
	total, users, err := userStore.List(ctx, where.P(params.Page, params.Size).S(columns...))
	if err != nil {
		log.Fatal(err)
	}

	// 3. 响应 = 数据 + 元数据。
	body := struct {
		Meta  page.Meta `json:"meta"`
		Items []*User   `json:"items"`
	}{
		Meta:  page.NewMeta(params, total),
		Items: users,
	}
	encoded, err := json.MarshalIndent(body, "", "  ")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(string(encoded))

	// 4. 白名单外的字段直接拒绝，不会拼进 SQL。
	if _, err = sortable.Parse("name;drop table users"); err != nil {
		fmt.Println("rejected:", err)
	}
}
