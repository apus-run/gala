#!/bin/bash

# Gala 多模块版本发布脚本
# 用法: ./scripts/release.sh v0.6.0

set -e

# 颜色定义
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# 打印带颜色的消息
print_info() {
    echo -e "${BLUE}[INFO]${NC} $1"
}

print_success() {
    echo -e "${GREEN}[SUCCESS]${NC} $1"
}

print_warning() {
    echo -e "${YELLOW}[WARNING]${NC} $1"
}

print_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

# 检查参数
if [ -z "$1" ]; then
    print_error "版本号不能为空"
    echo "用法: $0 <版本号> [选项]"
    echo "示例: $0 v0.6.0"
    echo "选项:"
    echo "  -y, --yes     自动确认发布"
    echo "  -h, --help    显示帮助信息"
    exit 1
fi

VERSION=$1
AUTO_CONFIRM=false

# 检查是否有第二个参数
if [ -n "$2" ]; then
    case "$2" in
        -y|--yes)
            AUTO_CONFIRM=true
            ;;
        -h|--help)
            print_info "用法: $0 <版本号> [选项]"
            echo "选项:"
            echo "  -y, --yes     自动确认发布"
            echo "  -h, --help    显示帮助信息"
            exit 0
            ;;
        *)
            print_error "未知选项: $2"
            exit 1
            ;;
    esac
fi

# 验证版本号格式
if [[ ! $VERSION =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
    print_error "版本号格式不正确"
    echo "正确格式: v0.6.0, v1.2.3"
    exit 1
fi

print_info "开始发布版本: $VERSION"
echo ""

# 检查是否有未提交的更改
if [ -n "$(git status --porcelain)" ]; then
    print_error "存在未提交的更改，请先提交代码"
    git status --porcelain
    exit 1
fi

# 获取所有模块列表
MODULES=$(find . -name "go.mod" -type f ! -path "./go.work*" | sed 's|/go.mod||' | sort)

if [ -z "$MODULES" ]; then
    print_error "未找到任何模块"
    exit 1
fi

print_info "发现以下模块:"
echo "$MODULES" | while read -r module; do
    echo "  - $module"
done
echo ""

# 读取当前分支
CURRENT_BRANCH=$(git rev-parse --abbrev-ref HEAD)
print_info "当前分支: $CURRENT_BRANCH"

# 确认发布
if [ "$AUTO_CONFIRM" = true ]; then
    print_info "自动确认模式: 跳过用户确认"
else
    read -p "确认发布到分支: $CURRENT_BRANCH ? (y/N): " -n 1 -r
    echo
    if [[ ! $REPLY =~ ^[Yy]$ ]]; then
        print_info "取消发布"
        exit 0
    fi
fi
echo ""

# 创建标签
print_info "创建标签..."
print_info "为根模块创建标签: $VERSION"
print_info "为每个子模块创建独立标签"
echo ""

# 转换模块路径为标签名
convert_to_tag() {
    local module_path=$1
    # 移除前面的 ./
    module_path=${module_path#./}
    # 如果是根模块，返回空
    if [ -z "$module_path" ] || [ "$module_path" = "." ]; then
        echo ""
    else
        # 保留 / 字符 (Git 标签支持 /)
        echo "$module_path"
    fi
}

TAGS_CREATED=0

# 为根模块创建标签
if git tag -l | grep -q "^${VERSION}$"; then
    print_warning "根模块标签已存在: $VERSION"
else
    git tag -a "$VERSION" -m "Release $VERSION - Gala $VERSION"
    print_success "创建根模块标签: $VERSION"
    TAGS_CREATED=$((TAGS_CREATED + 1))
fi

# 为每个子模块创建标签
for module in $MODULES; do
    if [ "$module" = "." ]; then
        continue
    fi

    MODULE_PATH=$(convert_to_tag "$module")  # 例如: components/db
    TAG_FULL="${MODULE_PATH}/${VERSION}"     # 例如: components/db/v0.6.2

    # 检查标签是否已存在
    if git tag -l | grep -q "^${TAG_FULL}$"; then
        print_warning "标签已存在: $TAG_FULL"
        continue
    fi

    # 创建 annotated tag
    git tag -a "$TAG_FULL" -m "Release $TAG_FULL"
    print_success "创建子模块标签: $TAG_FULL (适用于 $module)"
    TAGS_CREATED=$((TAGS_CREATED + 1))
done

if [ $TAGS_CREATED -eq 0 ]; then
    print_warning "没有创建新标签（可能都已存在）"
else
    echo ""
    print_info "推送标签到远程仓库..."
    git push --tags

    if [ $? -eq 0 ]; then
        print_success "所有标签推送成功"
    else
        print_error "推送标签失败"
        exit 1
    fi
fi

echo ""
print_success "发布完成！"
print_info "已发布的版本: $VERSION"
print_info "已发布到分支: $CURRENT_BRANCH"
print_info "模块总数: $(echo "$MODULES" | wc -l)"
echo ""

# 显示使用说明
print_info "其他项目使用方法:"
echo "  # 子模块独立标签（推荐）"
echo "  go get github.com/apus-run/gala/components/db@components/db/$VERSION"
echo "  go get github.com/apus-run/gala/components/cache@components/cache/$VERSION"
echo "  go get github.com/apus-run/gala/pkg/errorsx@pkg/errorsx/$VERSION"
echo ""
echo "  # 或在 go.mod 中添加"
echo "  require ("
echo "      github.com/apus-run/gala/components/db components/db/$VERSION"
echo "      github.com/apus-run/gala/pkg/errorsx pkg/errorsx/$VERSION"
echo "  )"
echo ""
print_warning "注意: 子模块标签格式为 {路径}/{版本}"
echo "  例如: components/db/v0.6.2 (components/db 模块)"
echo "        pkg/errorsx/v0.6.2 (pkg/errorsx 包)"
echo ""

# 提示用户更新文档
print_warning "请记得:"
echo "  1. 更新 README.md (如有版本信息)"
echo "  2. 更新 CHANGELOG.md (如有此文件)"
echo ""
