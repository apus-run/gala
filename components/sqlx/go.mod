module github.com/apus-run/gala/components/sqlx

go 1.25

replace github.com/apus-run/gala/components/sqlx => ./sqlx

require (
	github.com/jmoiron/sqlx v1.4.0
	github.com/xo/dburl v0.24.2
)

require github.com/mattn/go-sqlite3 v1.14.22 // test-only
