module github.com/apus-run/gala/components/backoff

go 1.25

replace github.com/apus-run/gala/components/backoff => ../backoff

require (
	github.com/cenk/backoff v2.2.1+incompatible
	github.com/stretchr/testify v1.10.0
)

require (
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/pmezard/go-difflib v1.0.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)
