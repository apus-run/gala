module github.com/apus-run/gala/components/db

go 1.25

require (
	github.com/apus-run/gala/pkg/lang v0.0.0-20251114123351-8359a2c70c91
	github.com/go-sql-driver/mysql v1.9.3
	github.com/pkg/errors v0.9.1
	github.com/stretchr/testify v1.11.1
	go.uber.org/mock v0.5.2
	gorm.io/driver/mysql v1.6.0
	gorm.io/driver/sqlite v1.6.0
	gorm.io/gorm v1.30.1
	gorm.io/plugin/dbresolver v1.6.2
)

require (
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/guregu/null v4.0.0+incompatible // indirect
	github.com/json-iterator/go v1.1.12 // indirect
	github.com/kr/text v0.2.0 // indirect
	github.com/modern-go/concurrent v0.0.0-20180306012644-bacd9c7ef1dd // indirect
	github.com/modern-go/reflect2 v1.0.2 // indirect
	github.com/munnerz/goautoneg v0.0.0-20191010083416-a7dc8b61c822 // indirect
	github.com/prometheus/client_model v0.6.2 // indirect
	github.com/prometheus/common v0.65.0 // indirect
	github.com/prometheus/procfs v0.16.1 // indirect
	golang.org/x/sys v0.33.0 // indirect
	google.golang.org/protobuf v1.36.6 // indirect
)

require (
	filippo.io/edwards25519 v1.1.0 // indirect
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/jinzhu/inflection v1.0.0 // indirect
	github.com/jinzhu/now v1.1.5 // indirect
	github.com/mattn/go-sqlite3 v1.14.22 // indirect
	github.com/pmezard/go-difflib v1.0.0 // indirect
	github.com/prometheus/client_golang v1.23.0
	golang.org/x/text v0.25.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)

replace github.com/apus-run/gala/components/db => ../db
