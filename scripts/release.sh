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
print_info "为所有模块创建统一版本标签: $VERSION"
print_warning "注意: 所有子模块共享相同的版本标签"
echo ""

# 检查标签是否已存在
if git tag -l | grep -q "^${VERSION}$"; then
    print_warning "标签已存在: $VERSION"
    read -p "是否重新创建标签? (y/N): " -n 1 -r
    echo
    if [[ ! $REPLY =~ ^[Yy]$ ]]; then
        print_info "取消发布"
        exit 0
    fi
    # 删除已存在的标签
    git tag -d "$VERSION" > /dev/null
    git push origin ":refs/tags/$VERSION" > /dev/null 2>&1 || true
fi

# 创建 annotated tag
git tag -a "$VERSION" -m "Release $VERSION - Gala $VERSION"
TAGS_CREATED=1

print_success "创建标签: $VERSION"
print_info "标签说明: 适用于所有 35 个模块"

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
echo "  # 明确指定版本（推荐）"
echo "  go get github.com/apus-run/gala/components/db@$VERSION"
echo "  go get github.com/apus-run/gala/components/cache@$VERSION"
echo "  go get github.com/apus-run/gala/pkg/errorsx@$VERSION"
echo ""
echo "  # 或在 go.mod 中添加"
echo "  require ("
echo "      github.com/apus-run/gala/components/db $VERSION"
echo "      github.com/apus-run/gala/pkg/errorsx $VERSION"
echo "  )"
echo ""

# 提示用户更新文档
print_warning "请记得:"
echo "  1. 更新 README.md (如有版本信息)"
echo "  2. 更新 CHANGELOG.md (如有此文件)"
echo ""
