// Command userlist 是一个接近真实业务的列表示例：状态过滤、关键词搜索、
// 白名单排序、分页，输出 meta + items。只依赖 gorm + SQLite。
//
//	go run ./examples/userlist
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/apus-run/gala/components/db/page"
	"github.com/apus-run/gala/components/db/store"
	"github.com/apus-run/gala/components/db/where"
)

// User 是示例业务模型。
type User struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	Name      string    `gorm:"size:64;index" json:"name"`
	Email     string    `gorm:"size:128;uniqueIndex" json:"email"`
	Status    string    `gorm:"size:16;index" json:"status"`
	CreatedAt time.Time `json:"created_at"`
}

const (
	statusActive   = "active"
	statusDisabled = "disabled"
)

// userSort 是这个接口允许排序的字段，请求里的 sort 只能落到这三列。
var userSort = where.Sortable{
	"id":         "id",
	"name":       "name",
	"created_at": "created_at",
}

// listQuery 是接口收到的查询参数，通常由 ShouldBindQuery 直接填充。
type listQuery struct {
	Page     int
	PageSize int
	Sort     string
	Status   string
	Keyword  string
}

// listUsers 是 service 层的写法：解析排序、拼过滤条件、取一页数据。
func listUsers(ctx context.Context, users *store.Store[User], q listQuery) (page.Meta, []*User, error) {
	columns, err := userSort.Parse(q.Sort)
	if err != nil {
		return page.Meta{}, nil, fmt.Errorf("bad request: %w", err) // 映射成 400
	}

	opts := where.P(q.Page, q.PageSize).S(columns...)
	if q.Status != "" {
		opts.F("status", q.Status) // 等值过滤
	}
	if q.Keyword != "" {
		opts.Q("name LIKE ?", "%"+q.Keyword+"%") // 关键词搜索
	}

	total, rows, err := users.List(ctx, opts)
	if err != nil {
		return page.Meta{}, nil, fmt.Errorf("list users: %w", err) // 映射成 500
	}

	return page.NewMeta(page.Params{Page: q.Page, Size: q.PageSize}, total), rows, nil
}

func main() {
	ctx := context.Background()
	db := open()
	seed(db)

	users := store.NewStore[User](dbProvider{db: db})

	// 1. 活跃用户，按注册时间倒序。
	run(ctx, users, "活跃用户，按注册时间倒序", listQuery{
		Page: 1, PageSize: 5, Sort: "-created_at", Status: statusActive,
	})

	// 2. 搜索「张」，按姓名升序，第 1 页。
	run(ctx, users, "搜索「张」，按姓名升序", listQuery{
		Page: 1, PageSize: 2, Sort: "name", Keyword: "张",
	})

	// 3. 同一个搜索的第 2 页。
	run(ctx, users, "搜索「张」的第 2 页", listQuery{
		Page: 2, PageSize: 2, Sort: "name", Keyword: "张",
	})

	// 4. 白名单外的排序字段直接被拒绝，不会拼进 SQL。
	run(ctx, users, "非法排序字段", listQuery{
		Page: 1, PageSize: 5, Sort: "email;drop table users",
	})
}

func run(ctx context.Context, users *store.Store[User], title string, q listQuery) {
	fmt.Printf("## %s\n", title)
	fmt.Printf("   page=%d page_size=%d sort=%q status=%q keyword=%q\n",
		q.Page, q.PageSize, q.Sort, q.Status, q.Keyword)

	meta, rows, err := listUsers(ctx, users, q)
	if err != nil {
		fmt.Printf("error: %v\n\n", err)
		return
	}

	body := struct {
		Meta  page.Meta `json:"meta"`
		Items []*User   `json:"items"`
	}{
		Meta:  meta,
		Items: rows,
	}
	encoded, err := json.MarshalIndent(body, "", "  ")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(string(encoded))
	fmt.Println()
}

func open() *gorm.DB {
	db, err := gorm.Open(sqlite.Open("file:userlist?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		log.Fatal(err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		log.Fatal(err)
	}
	sqlDB.SetMaxOpenConns(1)

	if err = db.AutoMigrate(&User{}); err != nil {
		log.Fatal(err)
	}
	return db
}

func seed(db *gorm.DB) {
	base := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	rows := []User{
		{Name: "张伟", Email: "zhangwei@example.com", Status: statusActive, CreatedAt: base.Add(1 * time.Hour)},
		{Name: "王芳", Email: "wangfang@example.com", Status: statusActive, CreatedAt: base.Add(2 * time.Hour)},
		{Name: "李娜", Email: "lina@example.com", Status: statusDisabled, CreatedAt: base.Add(3 * time.Hour)},
		{Name: "张敏", Email: "zhangmin@example.com", Status: statusActive, CreatedAt: base.Add(4 * time.Hour)},
		{Name: "刘洋", Email: "liuyang@example.com", Status: statusActive, CreatedAt: base.Add(5 * time.Hour)},
		{Name: "陈静", Email: "chenjing@example.com", Status: statusDisabled, CreatedAt: base.Add(6 * time.Hour)},
		{Name: "杨磊", Email: "yanglei@example.com", Status: statusActive, CreatedAt: base.Add(7 * time.Hour)},
		{Name: "赵丽", Email: "zhaoli@example.com", Status: statusActive, CreatedAt: base.Add(8 * time.Hour)},
		{Name: "张强", Email: "zhangqiang@example.com", Status: statusActive, CreatedAt: base.Add(9 * time.Hour)},
		{Name: "周杰", Email: "zhoujie@example.com", Status: statusDisabled, CreatedAt: base.Add(10 * time.Hour)},
		{Name: "吴丹", Email: "wudan@example.com", Status: statusActive, CreatedAt: base.Add(11 * time.Hour)},
		{Name: "徐明", Email: "xuming@example.com", Status: statusActive, CreatedAt: base.Add(12 * time.Hour)},
	}
	if err := db.Create(&rows).Error; err != nil {
		log.Fatal(err)
	}
}

type txKey struct{}

// dbProvider 把 *gorm.DB 适配成 store.Provider。
type dbProvider struct {
	db *gorm.DB
}

func (p dbProvider) DB(ctx context.Context, wheres ...where.Where) *gorm.DB {
	session := p.db
	if tx, ok := ctx.Value(txKey{}).(*gorm.DB); ok {
		session = tx
	}
	session = session.WithContext(ctx)

	for _, whr := range wheres {
		if whr != nil {
			session = whr.Where(session)
		}
	}
	return session
}

func (p dbProvider) TX(ctx context.Context, fn func(context.Context) error) error {
	return p.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return fn(context.WithValue(ctx, txKey{}, tx))
	})
}
