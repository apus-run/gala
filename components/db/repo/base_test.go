package repo

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"gorm.io/gorm"
)

type Item struct {
	ID    uint `gorm:"primaryKey"`
	Name  string
	Grp   string
	Score int
	Note  *string
}

func newItemRepo(t *testing.T) (Repository[Item], *gorm.DB) {
	t.Helper()
	db := setupTestDB(t)
	if err := db.AutoMigrate(&Item{}); err != nil {
		t.Fatal(err)
	}
	return NewGormRepository(db, Item{}, "items"), db
}

func seedItems(t *testing.T, db *gorm.DB) {
	t.Helper()
	note := "n"
	rows := []Item{
		{Name: "a", Grp: "x", Score: 10, Note: &note},
		{Name: "b", Grp: "x", Score: 20},
		{Name: "c", Grp: "y", Score: 30, Note: &note},
		{Name: "d", Grp: "y", Score: 40},
	}
	if err := db.Create(&rows).Error; err != nil {
		t.Fatal(err)
	}
}

func names(rows []Item) string {
	s := ""
	for _, r := range rows {
		s += r.Name
	}
	return s
}

func TestRepo_FindAndFirst(t *testing.T) {
	r, db := newItemRepo(t)
	ctx := context.Background()

	if _, err := r.FindByID(ctx, 1); !errors.Is(err, ErrRecordNotFound) {
		t.Fatalf("FindByID on empty = %v, want ErrRecordNotFound", err)
	}
	if _, err := r.First(ctx); !errors.Is(err, ErrRecordNotFound) {
		t.Fatalf("First on empty = %v, want ErrRecordNotFound", err)
	}
	if _, err := r.FindOrFail(ctx, 1); err == nil || errors.Is(err, ErrRecordNotFound) == false && err.Error() != "items with id 1 not found" {
		t.Fatalf("FindOrFail on empty = %v", err)
	}
	if _, err := r.FirstOrFail(ctx); err == nil || err.Error() != "no items found" {
		t.Fatalf("FirstOrFail on empty = %v", err)
	}

	seedItems(t, db)
	if got, err := r.FindByID(ctx, 2); err != nil || got.Name != "b" {
		t.Fatalf("FindByID(2) = %+v, %v", got, err)
	}
	if got, err := r.FindOrFail(ctx, 3); err != nil || got.Name != "c" {
		t.Fatalf("FindOrFail(3) = %+v, %v", got, err)
	}
	if got, err := r.First(ctx); err != nil || got.Name != "a" {
		t.Fatalf("First = %+v, %v", got, err)
	}
	if got, err := r.FirstOrFail(ctx); err != nil || got.Name != "a" {
		t.Fatalf("FirstOrFail = %+v, %v", got, err)
	}
	all, err := r.FindAll(ctx)
	if err != nil || len(all) != 4 {
		t.Fatalf("FindAll = %d rows, %v", len(all), err)
	}
}

func TestRepo_UpdateDelete(t *testing.T) {
	r, db := newItemRepo(t)
	ctx := context.Background()
	seedItems(t, db)

	if err := r.Update(ctx, 1, &Item{Name: "a2"}); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if got, _ := r.FindByID(ctx, 1); got.Name != "a2" {
		t.Fatalf("after Update name = %q", got.Name)
	}
	if err := r.Update(ctx, 999, &Item{Name: "zz"}); !errors.Is(err, ErrRecordNotFound) {
		t.Fatalf("Update missing = %v, want ErrRecordNotFound", err)
	}

	if err := r.Delete(ctx, 1); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if err := r.Delete(ctx, 1); !errors.Is(err, ErrRecordNotFound) {
		t.Fatalf("Delete again = %v, want ErrRecordNotFound", err)
	}
}

func TestRepo_WhereQueries(t *testing.T) {
	r, db := newItemRepo(t)
	ctx := context.Background()
	seedItems(t, db)

	cases := []struct {
		name string
		q    Query[Item]
		want string
	}{
		{"Where", r.Where("grp", "x"), "ab"},
		{"WhereIn", r.WhereIn("name", []any{"a", "d"}), "ad"},
		{"WhereNotIn", r.WhereNotIn("name", []any{"a", "d"}), "bc"},
		{"WhereBetween", r.WhereBetween("score", 15, 35), "bc"},
		{"WhereNull", r.WhereNull("note"), "bd"},
		{"WhereNotNull", r.WhereNotNull("note"), "ac"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rows, err := c.q.OrderByAsc("id").Find(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if names(rows) != c.want {
				t.Fatalf("got %q, want %q", names(rows), c.want)
			}
		})
	}
}

func TestRepo_RepositoryLevelBuilders(t *testing.T) {
	r, db := newItemRepo(t)
	ctx := context.Background()
	seedItems(t, db)

	rows, err := r.OrderBy("score", "DESC").Limit(2).Offset(1).Find(ctx)
	if err != nil || names(rows) != "cb" {
		t.Fatalf("OrderBy/Limit/Offset = %q, %v, want cb", names(rows), err)
	}
	rows, err = r.Limit(1).Find(ctx)
	if err != nil || len(rows) != 1 {
		t.Fatalf("Limit = %d, %v", len(rows), err)
	}
	if _, err := r.With("NoSuchRelation").Find(ctx); err == nil {
		t.Fatal("With(unknown relation) should fail")
	}
}

func TestRepo_QueryChain(t *testing.T) {
	r, db := newItemRepo(t)
	ctx := context.Background()
	seedItems(t, db)

	rows, err := r.Where("grp", "y").OrderByDesc("score").Find(ctx)
	if err != nil || names(rows) != "dc" {
		t.Fatalf("OrderByDesc = %q, %v", names(rows), err)
	}
	rows, err = r.Where("grp", "x").WhereNotNull("note").Find(ctx)
	if err != nil || names(rows) != "a" {
		t.Fatalf("chained = %q, %v", names(rows), err)
	}
	rows, err = r.Where("grp", "x").Limit(1).Offset(1).OrderByAsc("id").Find(ctx)
	if err != nil || names(rows) != "b" {
		t.Fatalf("Limit/Offset = %q, %v", names(rows), err)
	}
	rows, err = r.Where("grp", "x").Select("name").Distinct().OrderByAsc("name").Find(ctx)
	if err != nil || names(rows) != "ab" {
		t.Fatalf("Select/Distinct = %q, %v", names(rows), err)
	}
	if _, err := r.Where("grp", "x").With("NoSuchRelation").Find(ctx); err == nil {
		t.Fatal("With(unknown relation) should fail")
	}
}

func TestRepo_GroupByHaving(t *testing.T) {
	r, db := newItemRepo(t)
	ctx := context.Background()
	seedItems(t, db)

	type agg struct {
		Grp string
		N   int
	}
	var out []agg
	q := r.WhereNotNull("name").GroupBy("grp").Having("COUNT(*) >= ?", 2).Select("grp", "COUNT(*) AS n")
	g := q.(*GormQuery[Item])
	if err := g.db.Model(&Item{}).Order("`grp`").Scan(&out).Error; err != nil {
		t.Fatal(err)
	}
	if len(out) != 2 || out[0].N != 2 {
		t.Fatalf("GroupBy/Having = %+v", out)
	}

	// 多字段分组走 fields[1:] 分支。
	if _, err := r.WhereNotNull("name").GroupBy("grp", "name").Find(ctx); err != nil {
		t.Fatalf("GroupBy(2 fields): %v", err)
	}
}

func TestRepo_OrWhere(t *testing.T) {
	r, db := newItemRepo(t)
	ctx := context.Background()
	seedItems(t, db)

	rows, err := r.Where("name", "a").OrWhere("name", "d").OrderByAsc("id").Find(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if names(rows) != "ad" {
		t.Fatalf("Where(a).OrWhere(d) = %q, want ad", names(rows))
	}
}

func TestRepo_Terminals(t *testing.T) {
	r, db := newItemRepo(t)
	ctx := context.Background()

	if ok, err := r.Exists(ctx); err != nil || ok {
		t.Fatalf("Exists on empty = %v, %v", ok, err)
	}
	if ok, err := r.DoesNotExist(ctx); err != nil || !ok {
		t.Fatalf("DoesNotExist on empty = %v, %v", ok, err)
	}
	if _, err := r.Where("name", "x").First(ctx); !errors.Is(err, ErrRecordNotFound) {
		t.Fatalf("Query.First on empty = %v", err)
	}
	if _, err := r.Where("name", "x").FirstOrFail(ctx); err == nil || err.Error() != "no records found" {
		t.Fatalf("Query.FirstOrFail on empty = %v", err)
	}

	seedItems(t, db)

	if n, err := r.Count(ctx); err != nil || n != 4 {
		t.Fatalf("Count = %d, %v", n, err)
	}
	if ok, _ := r.Exists(ctx); !ok {
		t.Fatal("Exists = false")
	}
	if ok, _ := r.DoesNotExist(ctx); ok {
		t.Fatal("DoesNotExist = true")
	}

	q := r.Where("grp", "x")
	if n, err := q.Count(ctx); err != nil || n != 2 {
		t.Fatalf("Query.Count = %d, %v", n, err)
	}
	if ok, err := q.Exists(ctx); err != nil || !ok {
		t.Fatalf("Query.Exists = %v, %v", ok, err)
	}
	if ok, err := q.DoesNotExist(ctx); err != nil || ok {
		t.Fatalf("Query.DoesNotExist = %v, %v", ok, err)
	}
	if got, err := q.OrderByAsc("id").First(ctx); err != nil || got.Name != "a" {
		t.Fatalf("Query.First = %+v, %v", got, err)
	}
	if got, err := q.OrderByAsc("id").FirstOrFail(ctx); err != nil || got.Name != "a" {
		t.Fatalf("Query.FirstOrFail = %+v, %v", got, err)
	}
	vals, err := q.OrderByAsc("id").Pluck(ctx, "name")
	if err != nil || len(vals) != 2 {
		t.Fatalf("Query.Pluck = %v, %v", vals, err)
	}
}

func TestRepo_Pluck(t *testing.T) {
	r, db := newItemRepo(t)
	ctx := context.Background()
	seedItems(t, db)

	vals, err := r.Pluck(ctx, "name")
	if err != nil || len(vals) != 4 {
		t.Fatalf("Pluck = %v, %v", vals, err)
	}
	strs, err := r.PluckString(ctx, "name")
	if err != nil || len(strs) != 4 || strs[0] != "a" {
		t.Fatalf("PluckString = %v, %v", strs, err)
	}
	ints, err := r.PluckInt(ctx, "score")
	if err != nil || len(ints) != 4 || ints[3] != 40 {
		t.Fatalf("PluckInt = %v, %v", ints, err)
	}
}

func TestRepo_QueryUpdateDelete(t *testing.T) {
	r, db := newItemRepo(t)
	ctx := context.Background()
	seedItems(t, db)

	if err := r.Where("grp", "x").Update(ctx, map[string]any{"score": 99}); err != nil {
		t.Fatalf("Query.Update: %v", err)
	}
	if n, _ := r.Where("score", 99).Count(ctx); n != 2 {
		t.Fatalf("after Update, score=99 rows = %d, want 2", n)
	}
	if err := r.Where("grp", "y").Delete(ctx); err != nil {
		t.Fatalf("Query.Delete: %v", err)
	}
	if n, _ := r.Count(ctx); n != 2 {
		t.Fatalf("after Delete count = %d, want 2", n)
	}
}

func TestRepo_Batch(t *testing.T) {
	r, _ := newItemRepo(t)
	ctx := context.Background()

	rows := make([]Item, 250)
	for i := range rows {
		rows[i] = Item{Name: fmt.Sprintf("n%03d", i), Score: i}
	}
	if err := r.CreateBatch(ctx, rows); err != nil {
		t.Fatalf("CreateBatch: %v", err)
	}
	if n, _ := r.Count(ctx); n != 250 {
		t.Fatalf("count = %d, want 250", n)
	}

	if err := r.UpdateBatch(ctx, []any{1, 2}, []Item{{Name: "u1"}, {Name: "u2"}}); err != nil {
		t.Fatalf("UpdateBatch: %v", err)
	}
	if got, _ := r.FindByID(ctx, 2); got.Name != "u2" {
		t.Fatalf("after UpdateBatch name = %q", got.Name)
	}
	if err := r.UpdateBatch(ctx, []any{1}, []Item{{}, {}}); err == nil {
		t.Fatal("UpdateBatch with mismatched lengths should fail")
	}

	if err := r.DeleteBatch(ctx, []any{1, 2, 3}); err != nil {
		t.Fatalf("DeleteBatch: %v", err)
	}
	if n, _ := r.Count(ctx); n != 247 {
		t.Fatalf("after DeleteBatch count = %d, want 247", n)
	}
}

func TestRepo_IncrementDecrement(t *testing.T) {
	r, db := newItemRepo(t)
	ctx := context.Background()
	seedItems(t, db)

	if err := r.Increment(ctx, 1, "score", 5); err != nil {
		t.Fatal(err)
	}
	if got, _ := r.FindByID(ctx, 1); got.Score != 15 {
		t.Fatalf("after Increment score = %d, want 15", got.Score)
	}
	if err := r.Decrement(ctx, 1, "score", 3); err != nil {
		t.Fatal(err)
	}
	if got, _ := r.FindByID(ctx, 1); got.Score != 12 {
		t.Fatalf("after Decrement score = %d, want 12", got.Score)
	}
}

func TestRepo_WithTransaction(t *testing.T) {
	r, db := newItemRepo(t)
	ctx := context.Background()

	tx := db.Begin()
	txRepo := r.WithTransaction(tx)
	if err := txRepo.Create(ctx, &Item{Name: "in-tx"}); err != nil {
		t.Fatal(err)
	}
	if n, _ := txRepo.Count(ctx); n != 1 {
		t.Fatalf("count inside tx = %d, want 1", n)
	}
	tx.Rollback()
	if n, _ := r.Count(ctx); n != 0 {
		t.Fatalf("count after rollback = %d, want 0", n)
	}
}

// UpdateBatch 在 WithTransaction 的事务里调用时必须仍能成功（嵌套走 savepoint）。
func TestRepo_UpdateBatchInsideTransaction(t *testing.T) {
	r, db := newItemRepo(t)
	ctx := context.Background()
	seedItems(t, db)

	tx := db.Begin()
	defer tx.Rollback()
	if err := r.WithTransaction(tx).UpdateBatch(ctx, []any{1}, []Item{{Name: "nested"}}); err != nil {
		t.Fatalf("UpdateBatch inside a transaction: %v", err)
	}
}

// 仓库级入口（Where/WhereIn/...）与 Query 上的同名方法各自独立实现，需要分别覆盖。
func TestRepo_RepositoryLevelWhere(t *testing.T) {
	r, db := newItemRepo(t)
	ctx := context.Background()
	seedItems(t, db)

	cases := []struct {
		name string
		q    Query[Item]
		want string
	}{
		{"Where", r.Where("grp", "x"), "ab"},
		{"WhereIn", r.WhereIn("name", []any{"a", "d"}), "ad"},
		{"WhereNotIn", r.WhereNotIn("name", []any{"a", "d"}), "bc"},
		{"WhereBetween", r.WhereBetween("score", 15, 35), "bc"},
		{"WhereNull", r.WhereNull("note"), "bd"},
		{"WhereNotNull", r.WhereNotNull("note"), "ac"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rows, err := c.q.OrderByAsc("id").Find(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if names(rows) != c.want {
				t.Fatalf("got %q, want %q", names(rows), c.want)
			}
		})
	}

	rows, err := r.Offset(3).OrderBy("id", "ASC").Find(ctx)
	if err != nil || names(rows) != "d" {
		t.Fatalf("Offset(3) = %q, %v, want d", names(rows), err)
	}
}

// 表不存在时，非 NotFound 的数据库错误必须原样透出，而不是被映射成 ErrRecordNotFound。
func TestRepo_PropagatesNonNotFoundErrors(t *testing.T) {
	type Ghost struct {
		ID   uint
		Name string
	}
	db := setupTestDB(t)
	r := NewGormRepository(db, Ghost{}, "ghosts")
	ctx := context.Background()

	notNotFound := func(name string, err error) {
		t.Helper()
		if err == nil || errors.Is(err, ErrRecordNotFound) {
			t.Fatalf("%s = %v, want a raw database error", name, err)
		}
	}

	_, err := r.FindByID(ctx, 1)
	notNotFound("FindByID", err)
	_, err = r.FindOrFail(ctx, 1)
	notNotFound("FindOrFail", err)
	_, err = r.First(ctx)
	notNotFound("First", err)
	_, err = r.FirstOrFail(ctx)
	notNotFound("FirstOrFail", err)
	notNotFound("Update", r.Update(ctx, 1, &Ghost{Name: "x"}))
	notNotFound("Delete", r.Delete(ctx, 1))
	notNotFound("UpdateBatch", r.UpdateBatch(ctx, []any{1}, []Ghost{{Name: "x"}}))

	_, err = r.Where("id", 1).First(ctx)
	notNotFound("Query.First", err)
	_, err = r.Where("id", 1).FirstOrFail(ctx)
	notNotFound("Query.FirstOrFail", err)
	_, _, err = r.Where("id", 1).Paginate(1, 10).Execute(ctx)
	notNotFound("Paginate.Execute", err)
}

// GormQuery 自己的链式 Where* 与仓库级入口是两份实现，从仓库级入口起链再调用一遍。
func TestRepo_ChainedWhere(t *testing.T) {
	r, db := newItemRepo(t)
	ctx := context.Background()
	seedItems(t, db)

	cases := []struct {
		name string
		q    Query[Item]
		want string
	}{
		{"Where", r.Limit(10).Where("grp", "x"), "ab"},
		{"WhereIn", r.Limit(10).WhereIn("name", []any{"a", "d"}), "ad"},
		{"WhereNotIn", r.Limit(10).WhereNotIn("name", []any{"a", "d"}), "bc"},
		{"WhereBetween", r.Limit(10).WhereBetween("score", 15, 35), "bc"},
		{"WhereNull", r.Limit(10).WhereNull("note"), "bd"},
		{"WhereNotNull", r.Limit(10).WhereNotNull("note"), "ac"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rows, err := c.q.OrderByAsc("id").Find(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if names(rows) != c.want {
				t.Fatalf("got %q, want %q", names(rows), c.want)
			}
		})
	}
}

// Count 成功、取页失败：用不存在的排序列让第二条查询报错，错误必须透出。
func TestRepo_PaginateRowsQueryError(t *testing.T) {
	r, db := newItemRepo(t)
	seedItems(t, db)

	meta, rows, err := r.OrderBy("no_such_column", "ASC").Paginate(1, 10).Execute(context.Background())
	if err == nil {
		t.Fatal("Execute should fail when the page query fails")
	}
	if meta != nil || rows != nil {
		t.Fatalf("meta = %v, rows = %v, want nil on error", meta, rows)
	}
}

// ---- 字段名注入：字段名始终作为一个带引号的标识符进入 SQL，永远不会被当作 SQL 片段 ----

var hostile = []string{
	"name; DROP TABLE items",
	"note IS NULL OR 1=1 OR note",
	"name FROM items --",
	"score = 999, name",
	"name) OR 1=1 --",
	"name` OR 1=1 OR `name",
	"name\" OR 1=1 OR \"name",
}

// 每个接收字段名的入口：恶意字段名只会被当成一个不存在的列，查询失败，
// 生成的 SQL 里它整体在一对引号之内，数据不被修改，也不会多返回行。
func TestRepo_HostileFieldNamesAreQuoted(t *testing.T) {
	r, db := newItemRepo(t)
	ctx := context.Background()
	seedItems(t, db)

	var sqls []string
	lr := NewGormRepository(db.Session(&gorm.Session{Logger: captureLogger{&sqls}}), Item{}, "items")

	for _, f := range hostile {
		t.Run(fmt.Sprintf("%q", f), func(t *testing.T) {
			checks := map[string]func() error{
				"Where":        func() error { _, err := lr.Where(f, 1).Find(ctx); return err },
				"WhereIn":      func() error { _, err := lr.WhereIn(f, []any{1}).Find(ctx); return err },
				"WhereNotIn":   func() error { _, err := lr.WhereNotIn(f, []any{1}).Find(ctx); return err },
				"WhereBetween": func() error { _, err := lr.WhereBetween(f, 1, 2).Find(ctx); return err },
				"WhereNull":    func() error { _, err := lr.WhereNull(f).Find(ctx); return err },
				"WhereNotNull": func() error { _, err := lr.WhereNotNull(f).Find(ctx); return err },
				"OrWhere":      func() error { _, err := lr.Where("name", "a").OrWhere(f, 1).Find(ctx); return err },
				"OrderBy":      func() error { _, err := lr.OrderBy(f, "ASC").Find(ctx); return err },
				"OrderByDesc":  func() error { _, err := lr.Where("name", "a").OrderByDesc(f).Find(ctx); return err },
				"GroupBy":      func() error { _, err := lr.Where("name", "a").GroupBy(f).Find(ctx); return err },
				"Pluck":        func() error { _, err := lr.Pluck(ctx, f); return err },
				"PluckString":  func() error { _, err := lr.PluckString(ctx, f); return err },
				"PluckInt":     func() error { _, err := lr.PluckInt(ctx, f); return err },
				"QueryPluck":   func() error { _, err := lr.Where("name", "a").Pluck(ctx, f); return err },
				"Increment":    func() error { return lr.Increment(ctx, 1, f, 1) },
				"Decrement":    func() error { return lr.Decrement(ctx, 1, f, 1) },
			}
			for name, fn := range checks {
				sqls = nil
				if err := fn(); err == nil {
					t.Errorf("%s(%q) succeeded, want an unknown-column error", name, f)
				}
				for _, q := range sqls {
					if strings.Contains(strings.ToUpper(q), "DROP TABLE") && !strings.Contains(q, "`name; DROP TABLE items`") {
						t.Errorf("%s: field escaped its quotes: %s", name, q)
					}
					if strings.Contains(q, " OR 1=1") && !strings.Contains(q, "`") {
						t.Errorf("%s: unquoted payload: %s", name, q)
					}
				}
			}
		})
	}

	// 没有任何副作用：表还在，数据原封不动。
	if n, err := r.Count(ctx); err != nil || n != 4 {
		t.Fatalf("count = %d, %v, want 4", n, err)
	}
	if got, _ := r.FindByID(ctx, 1); got.Score != 10 || got.Name != "a" {
		t.Fatalf("row 1 modified: %+v", got)
	}
}

// 主键参数不能被当作原始 SQL：id 为字符串时 gorm 的 First(&x, id) 会拼进 WHERE。
func TestRepo_PrimaryKeyIsNeverRawSQL(t *testing.T) {
	r, db := newItemRepo(t)
	ctx := context.Background()
	seedItems(t, db)

	if got, err := r.FindByID(ctx, "0 OR 1=1"); err == nil {
		t.Fatalf("FindByID(injection) returned %+v, want not found", got)
	}
	if err := r.Delete(ctx, "0 OR 1=1"); !errors.Is(err, ErrRecordNotFound) {
		t.Fatalf("Delete(injection) = %v, want ErrRecordNotFound", err)
	}
	if n, _ := r.Count(ctx); n != 4 {
		t.Fatalf("count = %d, want 4 (nothing deleted)", n)
	}
}

func TestRepo_ValidIdentifiersAndDirection(t *testing.T) {
	r, db := newItemRepo(t)
	ctx := context.Background()
	seedItems(t, db)

	rows, err := r.Where("items.grp", "x").OrderBy("items.score", "desc").Find(ctx)
	if err != nil || names(rows) != "ba" {
		t.Fatalf("qualified + lower-case direction = %q, %v", names(rows), err)
	}
	// 方向只认 DESC，其余（包括注入尝试）一律正序，字符串本身从不进入 SQL。
	var sqls []string
	lr := NewGormRepository(db.Session(&gorm.Session{Logger: captureLogger{&sqls}}), Item{}, "items")
	for _, d := range []string{"", "ASC; DROP TABLE items", "sideways", "ASC, name"} {
		sqls = nil
		rows, err := lr.OrderBy("score", d).Find(ctx)
		if err != nil || names(rows) != "abcd" {
			t.Errorf("OrderBy direction %q = %q, %v, want ascending abcd", d, names(rows), err)
		}
		for _, q := range sqls {
			if d != "" && strings.Contains(q, d) {
				t.Errorf("direction %q leaked into SQL: %s", d, q)
			}
		}
	}
}

// 一条失败的链不会污染仓库：下一次查询照常工作。
func TestRepo_FailedChainDoesNotPoisonRepo(t *testing.T) {
	r, db := newItemRepo(t)
	ctx := context.Background()
	seedItems(t, db)

	if _, err := r.Where("bad field", 1).Where("name", "a").Limit(1).Find(ctx); err == nil {
		t.Fatal("query on an unknown column should fail")
	}
	rows, err := r.Where("name", "a").Find(ctx)
	if err != nil || len(rows) != 1 {
		t.Fatalf("repo poisoned by a failed chain: %d rows, %v", len(rows), err)
	}
	if n, err := r.Count(ctx); err != nil || n != 4 {
		t.Fatalf("Count after failed chain = %d, %v", n, err)
	}
}

func TestRepo_GroupByNoFields(t *testing.T) {
	r, db := newItemRepo(t)
	seedItems(t, db)
	rows, err := r.Where("grp", "x").GroupBy().OrderByAsc("id").Find(context.Background())
	if err != nil || names(rows) != "ab" {
		t.Fatalf("GroupBy() with no fields = %q, %v, want a no-op", names(rows), err)
	}
}

// Exists 只取一行，不数全部匹配行。
func TestRepo_ExistsSelectsOneRow(t *testing.T) {
	_, db := newItemRepo(t)
	ctx := context.Background()

	var sqls []string
	logged := db.Session(&gorm.Session{Logger: captureLogger{&sqls}})
	lr := NewGormRepository(logged, Item{}, "items")

	if ok, err := lr.Exists(ctx); err != nil || ok {
		t.Fatalf("Exists on empty = %v, %v", ok, err)
	}
	seedItems(t, db)
	if ok, err := lr.Where("grp", "y").Exists(ctx); err != nil || !ok {
		t.Fatalf("Query.Exists = %v, %v", ok, err)
	}
	if ok, err := lr.Where("grp", "nobody").Exists(ctx); err != nil || ok {
		t.Fatalf("Query.Exists(no match) = %v, %v", ok, err)
	}
	for _, q := range sqls {
		if strings.Contains(strings.ToLower(q), "count(") {
			t.Errorf("Exists must not COUNT: %s", q)
		}
		if !strings.Contains(strings.ToLower(q), "limit 1") {
			t.Errorf("Exists must LIMIT 1: %s", q)
		}
	}
	if len(sqls) == 0 {
		t.Fatal("no SQL captured")
	}
}

// GroupBy 与 Pluck 改用 clause 之后，合法列名、限定列名、多字段分组、Distinct 仍然正常。
func TestRepo_GroupByAndPluckWithClauses(t *testing.T) {
	r, db := newItemRepo(t)
	ctx := context.Background()
	seedItems(t, db)

	type agg struct {
		Grp string
		N   int
	}
	var out []agg
	q := r.WhereNotNull("name").GroupBy("items.grp").Having("COUNT(*) >= ?", 2).Select("grp", "COUNT(*) AS n")
	if err := q.(*GormQuery[Item]).db.Model(&Item{}).Order("grp").Scan(&out).Error; err != nil {
		t.Fatal(err)
	}
	if len(out) != 2 || out[0].N != 2 || out[1].Grp != "y" {
		t.Fatalf("GroupBy(qualified)+Having = %+v", out)
	}

	vals, err := r.Where("grp", "x").Select("grp").Distinct().Pluck(ctx, "grp")
	if err != nil || len(vals) != 1 {
		t.Fatalf("Distinct Pluck = %v, %v, want one value", vals, err)
	}
	strs, err := r.PluckString(ctx, "items.name")
	if err != nil || len(strs) != 4 {
		t.Fatalf("PluckString(qualified) = %v, %v", strs, err)
	}
}
