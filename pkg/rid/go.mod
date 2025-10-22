module github.com/apus-run/gala/pkg/rid

go 1.25

replace (
	github.com/apus-run/gala/pkg/id => ../id
	github.com/apus-run/gala/pkg/rid => ../rid
)

require (
	github.com/apus-run/gala/pkg/id v0.0.0-00010101000000-000000000000
	github.com/stretchr/testify v1.11.1
)

require (
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/pmezard/go-difflib v1.0.0 // indirect
	github.com/sony/sonyflake/v2 v2.2.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)
