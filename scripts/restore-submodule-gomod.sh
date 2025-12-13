#!/bin/bash

# 恢复子模块 go.mod 中的 replace 指令
# 用于本地开发时恢复 replace 指令

set -e

echo "恢复子模块 go.mod 中的 replace 指令..."

# 查找所有子模块的 go.mod 文件
MODULES=$(find components pkg options -name "go.mod" -type f | sort)

RESTORED=0

for mod in $MODULES; do
    # 获取模块路径
    MODULE_DIR=$(dirname "$mod")
    MODULE_NAME=$(basename "$MODULE_DIR")

    # 构建 replace 指令
    # 对于 components/xxx 模块，replace 指向 ../xxx
    # 对于 pkg/xxx 模块，replace 指向 ../xxx

    # 跳过根模块
    if [ "$MODULE_DIR" = "." ] || [ "$MODULE_DIR" = "components" ] || [ "$MODULE_DIR" = "pkg" ] || [ "$MODULE_DIR" = "options" ]; then
        continue
    fi

    # 获取模块路径相对于根目录的路径
    REL_PATH=$(echo "$MODULE_DIR" | sed 's|^\./||')

    # 构建 replace 指令
    if [ "$MODULE_DIR" = "options" ]; then
        REPLACE_PATH="../options"
    else
        REPLACE_PATH="../$(basename $MODULE_DIR)"
    fi

    FULL_MODULE="github.com/apus-run/gala/$REL_PATH"

    # 检查是否已经存在 replace 指令
    if grep -q "replace.*$FULL_MODULE" "$mod"; then
        echo "已存在: $mod"
        continue
    fi

    echo "恢复: $mod"

    # 在文件末尾添加 replace 指令
    echo "" >> "$mod"
    echo "replace $FULL_MODULE => $REPLACE_PATH" >> "$mod"

    RESTORED=$((RESTORED + 1))
done

echo ""
echo "恢复完成！"
echo "已恢复 $RESTORED 个子模块的 go.mod 文件"
echo ""
echo "注意: 这些 replace 指令用于本地开发"
echo "     发布前需要使用 scripts/clean-submodule-gomod.sh 清理"
