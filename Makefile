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

# 代码规范检查
.PHONY: lint
lint:
	@golangci-lint run -c ./scripts/lint/.golangci.yaml ./...

# 生成go代码
.PHONY: gen
gen:
	@go generate ./...
