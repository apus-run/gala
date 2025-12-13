#!/bin/bash

# 清理子模块 go.mod 中的 replace 指令
# 用于发布前清理本地开发时的 replace 指令

set -e

echo "清理子模块 go.mod 中的 replace 指令..."

# 查找所有子模块的 go.mod 文件
MODULES=$(find components pkg options -name "go.mod" -type f | sort)

CLEANED=0

for mod in $MODULES; do
    # 检查是否包含 replace 指令
    if grep -q "replace.*gala" "$mod"; then
        echo "清理: $mod"

        # 移除 replace 指令
        # 保留 replace 指令但注释掉非 gala 的
        # 或者完全移除
        # 这里我们选择完全移除包含 gala 的 replace 指令

        # 备份原文件
        cp "$mod" "${mod}.bak"

        # 移除包含 gala 的 replace 指令行
        grep -v "replace.*gala" "${mod}.bak" > "$mod"

        # 删除备份文件
        rm "${mod}.bak"

        CLEANED=$((CLEANED + 1))
    fi
done

echo ""
echo "清理完成！"
echo "已清理 $CLEANED 个子模块的 go.mod 文件"
echo ""
echo "注意: 请检查清理后的文件，确保无误后提交"
echo "     如果需要恢复，请使用 git checkout 恢复文件"
