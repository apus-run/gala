# 初始化项目环境
.PHONY: setup
setup:
	@sh ./scripts/setup.sh

# 格式化代码
.PHONY: fmt
fmt:
	@goimports -l -w $$(find . -type f -name '*.go' -not -path "./.idea/*")
	@gofumpt -l -w $$(find . -type f -name '*.go' -not -path "./.idea/*")

# 清理项目依赖
.PHONY: tidy
tidy:
	@find . -name go.mod -not -path './.git/*' -print | sort | while read -r mod; do \
		dir=$$(dirname "$$mod"); \
		echo "==> $$dir"; \
		(cd "$$dir" && go mod tidy) || exit 1; \
	done
	@go work sync

.PHONY: check
check:
	@$(MAKE) --no-print-directory fmt
	@$(MAKE) --no-print-directory tidy

# 测试 ginx 的全部 go 模块
.PHONY: test-ginx
test-ginx:
	@bash ./scripts/test-modules.sh components/ginx

# 测试 authz 的全部 go 模块
.PHONY: test-authz
test-authz:
	@bash ./scripts/test-modules.sh components/authz -short

# 测试 fileserver 核心模块和 S3 对象存储模块
.PHONY: test-fileserver
test-fileserver:
	@cd components/fileserver && go vet ./... && go test -count=1 ./...
	@cd components/fileserver/objectstore/s3 && go vet ./... && go test -count=1 ./...

# 代码规范检查
.PHONY: lint
lint:
	@golangci-lint run -c ./scripts/lint/.golangci.yaml ./...

# 生成go代码
.PHONY: gen
gen:
	@go generate ./...
