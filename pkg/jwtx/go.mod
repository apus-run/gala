module github.com/apus-run/gala/pkg/jwtx

go 1.25

replace github.com/apus-run/gala/pkg/jwtx => ../jwtx

require (
	github.com/golang-jwt/jwt/v5 v5.3.0
	github.com/stretchr/testify v1.11.1
	golang.org/x/crypto v0.43.0
)

require (
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/pmezard/go-difflib v1.0.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)
