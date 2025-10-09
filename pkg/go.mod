module github.com/apus-run/gala/pkg

go 1.25

replace github.com/apus-run/gala/pkg => ../pkg

require (
	github.com/bytedance/sonic v1.14.0
	github.com/davecgh/go-spew v1.1.2-0.20180830191138-d8f796af33cc
	github.com/go-playground/locales v0.14.1
	github.com/go-playground/universal-translator v0.18.1
	github.com/go-playground/validator/v10 v10.27.0
	github.com/golang-jwt/jwt/v5 v5.3.0
	github.com/google/uuid v1.6.0
	github.com/guregu/null v4.0.0+incompatible
	github.com/json-iterator/go v1.1.12
	github.com/mitchellh/go-wordwrap v1.0.1
	github.com/onsi/gomega v1.38.0
	github.com/panjf2000/ants/v2 v2.11.3
	github.com/pkg/errors v0.9.1
	github.com/sony/sonyflake/v2 v2.2.0
	github.com/spf13/cast v1.9.2
	github.com/stretchr/testify v1.10.0
	github.com/tkuchiki/go-timezone v0.2.3
	golang.org/x/crypto v0.40.0
	golang.org/x/sync v0.16.0
	golang.org/x/text v0.28.0
)

require (
	github.com/bytedance/sonic/loader v0.3.0 // indirect
	github.com/cloudwego/base64x v0.1.5 // indirect
	github.com/gabriel-vasile/mimetype v1.4.8 // indirect
	github.com/google/go-cmp v0.7.0 // indirect
	github.com/klauspost/cpuid/v2 v2.2.7 // indirect
	github.com/leodido/go-urn v1.4.0 // indirect
	github.com/modern-go/concurrent v0.0.0-20180306012644-bacd9c7ef1dd // indirect
	github.com/modern-go/reflect2 v1.0.2 // indirect
	github.com/pmezard/go-difflib v1.0.1-0.20181226105442-5d4384ee4fb2 // indirect
	github.com/twitchyliquid64/golang-asm v0.15.1 // indirect
	golang.org/x/arch v0.8.0 // indirect
	golang.org/x/net v0.42.0 // indirect
	golang.org/x/sys v0.35.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)
