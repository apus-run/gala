#!/bin/bash
set -e

[ -z "$1" ] && echo "❌ 版本号不能为空" && exit 1

VERSION=$1
AUTO_CONFIRM=false

[ -n "$2" ] && case "$2" in
    -y|--yes) AUTO_CONFIRM=true ;;
    -h|--help)
        echo "用法: $0 <版本号> [-y]"
        echo "示例: $0 v0.6.3"
        exit 0
        ;;
    *) echo "❌ 未知选项: $2" && exit 1 ;;
esac

[[ ! $VERSION =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]] && echo "❌ 版本格式: v0.6.0, v1.2.3" && exit 1

[ -n "$(git status --porcelain)" ] && echo "❌ 有未提交更改" && exit 1

# 获取所有模块列表
MODULES=$(find . -name "go.mod" -type f ! -path "./go.work*" | sed 's|/go.mod||' | sort)

[ -z "$MODULES" ] && echo "❌ 未找到模块" && exit 1

CURRENT_BRANCH=$(git rev-parse --abbrev-ref HEAD)

if [ "$AUTO_CONFIRM" != true ]; then
    read -p "发布到 $CURRENT_BRANCH? (y/N): " -n 1 -r
    echo
    [[ ! $REPLY =~ ^[Yy]$ ]] && exit 0
fi

echo ""

# 创建标签
echo "📦 创建标签: $VERSION"

convert_to_prefix() {
    local module_path=$1
    module_path=${module_path#./}
    [ -z "$module_path" ] || [ "$module_path" = "." ] && echo "" || echo "$module_path"
}

TAGS_CREATED=0

# 根模块标签
if git tag -l | grep -q "^${VERSION}$"; then
    echo "⚠️  标签已存在: $VERSION"
else
    git tag -a "$VERSION" -m "Release $VERSION"
    echo "✅ 根模块标签: $VERSION"
    TAGS_CREATED=1
fi

# 子模块标签
for module in $MODULES; do
    [ "$module" = "." ] && continue

    PREFIX=$(convert_to_prefix "$module")
    TAG_NAME="${PREFIX}/${VERSION}"

    git tag -l | grep -q "^${TAG_NAME}$" && continue

    git tag -a "$TAG_NAME" -m "Release $TAG_NAME"
    TAGS_CREATED=$((TAGS_CREATED + 1))
done

if [ $TAGS_CREATED -eq 0 ]; then
    echo "⚠️  没有创建新标签"
    exit 0
fi

echo "📤 推送标签..."
git push --tags
echo "✅ 发布完成: $VERSION"

# 使用方法
echo ""
echo "💡 使用:"
echo "  go get github.com/apus-run/gala/components/db@components/db/$VERSION"
echo ""
