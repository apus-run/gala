package sqlx

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"testing/synctest"
	"time"
)

type scalarModel int

func (scalarModel) TableName() string { return "users" }
func (scalarModel) KeyName() string   { return "id" }

type embeddedModel struct {
	*valueModel
}

func (embeddedModel) TableName() string { return "value_models" }
func (embeddedModel) KeyName() string   { return "id" }

type promotedModel struct {
	*valueModel
}

type nullableModel struct {
	ID   int64   `db:"id"`
	Name *string `db:"name"`
}

func (nullableModel) TableName() string { return "value_models" }
func (nullableModel) KeyName() string   { return "id" }

func TestInvalidModels(t *testing.T) {
	for _, tc := range []struct {
		name  string
		model Modeler
		error string
	}{
		{"nil", nil, "nil model"},
		{"typed_nil", (*valueModel)(nil), "nil model"},
		{"non_struct", scalarModel(1), "must be a struct"},
		{"nil_embedded", embeddedModel{}, "nil embedded pointer"},
		{"nil_promoted_receiver", promotedModel{}, "nil embedded pointer"},
		{"missing_key", &brokenModel{ID: 1}, `key column "missing" not found`},
		{"nested", &nestedModel{ID: 1}, "nested struct field"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := newTestDB(t)
			tx, err := db.Beginx()
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = tx.Rollback() })
			for _, op := range []struct {
				name string
				call func(Modeler) (sql.Result, error)
			}{
				{"DB.Insert", db.Insert}, {"DB.Update", db.Update},
				{"Tx.Insert", tx.Insert}, {"Tx.Update", tx.Update},
			} {
				t.Run(op.name, func(t *testing.T) {
					result, err := op.call(tc.model)
					if result != nil || err == nil || !strings.Contains(err.Error(), tc.error) {
						t.Fatalf("got (%v, %v), want nil result and error containing %q", result, err, tc.error)
					}
				})
			}
			// Invalid input must not leave the shared mapper locked or the transaction unusable.
			if _, err := tx.Insert(&User{Name: "valid", Email: "a@x.com"}); err != nil {
				t.Fatalf("insert after invalid model: %v", err)
			}
		})
	}
}

func TestInsertUpdateEmbeddedAndNullableFields(t *testing.T) {
	db := newTestDB(t)
	mustExec(t, db, `CREATE TABLE value_models (id INTEGER PRIMARY KEY, name TEXT)`)
	m := embeddedModel{&valueModel{ID: 1, Name: "before"}}
	if _, err := db.Insert(m); err != nil {
		t.Fatal(err)
	}
	m.Name = "after"
	if _, err := db.Update(m); err != nil {
		t.Fatal(err)
	}
	var name string
	if err := db.Get(&name, `SELECT name FROM value_models WHERE id = 1`); err != nil || name != "after" {
		t.Fatalf("embedded field: name=%q, err=%v", name, err)
	}
	// A nil leaf pointer is a SQL NULL, unlike a nil pointer on the traversal path.
	if _, err := db.Insert(nullableModel{ID: 2}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Update(nullableModel{ID: 1}); err != nil {
		t.Fatal(err)
	}
	var nulls int
	if err := db.Get(&nulls, `SELECT COUNT(*) FROM value_models WHERE name IS NULL`); err != nil || nulls != 2 {
		t.Fatalf("nullable fields: null rows=%d, err=%v", nulls, err)
	}
}

func TestNewDBConnectionLimits(t *testing.T) {
	for _, tc := range []struct {
		name     string
		options  []Option
		wantOpen int
		wantIdle int
	}{
		{"defaults", nil, 25, 5},
		{"positive", []Option{WithMaxOpenConns(7), WithMaxIdleConns(3)}, 7, 3},
		{"zero", []Option{WithMaxOpenConns(0), WithMaxIdleConns(0)}, 0, 0},
		{"negative", []Option{WithMaxOpenConns(-1), WithMaxIdleConns(-1)}, 0, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p, err := NewDB(append(tc.options, WithDSN("sqlite3::memory:"))...)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = p.Close() })
			db := p.DB(context.Background())
			var conns []*sql.Conn
			for range 6 {
				conn, err := db.Conn(context.Background())
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = conn.Close() })
				conns = append(conns, conn)
			}
			for _, conn := range conns {
				if err := conn.Close(); err != nil {
					t.Fatal(err)
				}
			}
			stats := db.Stats()
			if stats.MaxOpenConnections != tc.wantOpen || stats.Idle != tc.wantIdle {
				t.Fatalf("pool stats: %+v; want max open=%d, idle=%d", stats, tc.wantOpen, tc.wantIdle)
			}
		})
	}
}

func TestNewDBConnectionExpiry(t *testing.T) {
	for _, kind := range []string{"lifetime", "idle_time"} {
		for _, duration := range []time.Duration{-time.Second, 0, time.Second} {
			t.Run(kind+"/"+duration.String(), func(t *testing.T) {
				synctest.Test(t, func(t *testing.T) {
					opts := []Option{WithDSN("sqlite3::memory:")}
					if kind == "lifetime" {
						opts = append(opts, WithConnMaxLifetime(duration), WithConnMaxIdleTime(0))
					} else {
						opts = append(opts, WithConnMaxIdleTime(duration), WithConnMaxLifetime(0))
					}
					p, err := NewDB(opts...)
					if err != nil {
						t.Fatal(err)
					}
					defer p.Close()
					db := p.DB(context.Background())
					// Fake time also exceeds both package defaults, without a wall-clock wait.
					time.Sleep(time.Hour)
					synctest.Wait()
					stats := db.Stats()
					var wantLifetime, wantIdle int64
					wantOpen := 1
					if duration > 0 {
						wantOpen = 0
						if kind == "lifetime" {
							wantLifetime = 1
						} else {
							wantIdle = 1
						}
					}
					if stats.OpenConnections != wantOpen || stats.MaxLifetimeClosed != wantLifetime || stats.MaxIdleTimeClosed != wantIdle {
						t.Fatalf("pool stats: %+v; want open=%d, lifetime closed=%d, idle closed=%d", stats, wantOpen, wantLifetime, wantIdle)
					}
				})
			})
		}
	}
}
