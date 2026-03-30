#!/bin/bash
# PixHost 批量上传脚本 (动态域名校准版)
# 用法: ./PixHostUpload.sh <图片目录路径>

# 依赖检查
require_commands() {
    local missing=()
    local cmd
    for cmd in curl file jq; do
        if ! command -v "$cmd" >/dev/null 2>&1; then
            missing+=("$cmd")
        fi
    done
    if [ ${#missing[@]} -gt 0 ]; then
        echo "错误: 缺少依赖 [${missing[*]}]"
        echo "提示: 请通过 Dockerfile 提供上述命令后再运行上传脚本"
        exit 1
    fi
}

# 参数检查
if [ -z "$1" ]; then
    echo "错误: 必须指定图片目录路径"
    echo "用法: $0 <图片目录路径>"
    exit 1
fi

DIR="$1"
if [ ! -d "$DIR" ]; then
    echo "错误: 目录不存在 [$DIR]"
    exit 1
fi

# 文件验证
validate_file() {
    local file="$1"
    [[ ! -f "$file" ]] && { echo "警告: 文件不存在 [$file]"; return 1; }
    file "$file" | grep -qiE 'image|bitmap' || { echo "警告: 非图片文件 [$file]"; return 1; }
    [ $(du -m "$file" | cut -f1) -gt 10 ] && { echo "警告: 文件过大 (>10MB) [$file]"; return 1; }
    return 0
}

# 上传函数 (内部集成动态转换逻辑)
upload_image() {
    local image="$1"
    local response show_url th_url direct_url

    # 发起上传请求
    response=$(curl -s "https://api.pixhost.to/images" \
        -H "Accept: application/json" \
        -F "img=@$image" \
        -F "content_type=0" \
        -F "max_th_size=420" 2>&1) || {
        return 1
    }

    # 验证 JSON
    if ! jq -e . >/dev/null 2>&1 <<<"$response"; then
        return 1
    fi

    # 1. 同时提取 show_url 和 th_url
    show_url=$(jq -r '.show_url // empty' <<<"$response")
    th_url=$(jq -r '.th_url // empty' <<<"$response")

    if [ -z "$show_url" ] || [ -z "$th_url" ]; then
        return 1
    fi

    # 2. 动态逻辑 A: 将路径从 /thumbs/ 替换为 /images/
    direct_url="${th_url/\/thumbs\//\/images\/}"

    # 3. 动态逻辑 B: 强制域名校准 (tN -> imgN)
    # 使用正则表达式匹配 https://t数字.pixhost.to 并替换为 https://img数字.pixhost.to
    if [[ "$direct_url" =~ https://t([0-9]+)\.pixhost\.to ]]; then
        local node_num="${BASH_REMATCH[1]}"
        direct_url=$(echo "$direct_url" | sed -E "s|https://t${node_num}\.pixhost\.to|https://img${node_num}\.pixhost\.to|")
    fi

    # 输出最终确定的直链
    echo "$direct_url"
}

# 主流程
main() {
    require_commands
    local total=0 success=0
    local bbcode_links=()
    local direct_links=()

    # 统计文件
    total=$(find "$DIR" -maxdepth 1 -type f \( -iname "*.jpg" -o -iname "*.jpeg" -o -iname "*.png" -o -iname "*.gif" \) | wc -l)
    [ "$total" -eq 0 ] && {
        echo "警告: 未找到有效图片文件"
        exit 0
    }

    echo "开始处理 $total 个文件..."

    # 处理文件 (使用临时文件保存结果避免子 Shell 变量丢失)
    while IFS= read -r image; do
        validate_file "$image" || continue
        
        # 调用上传并获取校准后的直链
        if direct_url=$(upload_image "$image"); then
            ((success++))
            bbcode_links+=("[img]$direct_url[/img]")
            direct_links+=("$direct_url")
            echo "已上传并校准域名: $(basename "$image")"
        else
            echo "上传失败: $(basename "$image")"
        fi
    done < <(find "$DIR" -maxdepth 1 -type f \( -iname "*.jpg" -o -iname "*.jpeg" -o -iname "*.png" -o -iname "*.gif" \) | sort)

    # 统一输出结果
    echo
    if [ "$success" -gt 0 ]; then
        echo "========================================"
        echo "bbcode代码链接："
        printf "%s\n" "${bbcode_links[@]}"
        
        echo
        echo "图片直链 (已自动识别并校准存储节点)："
        printf "%s\n" "${direct_links[@]}"
        echo "========================================"
    fi

    # 结果报告
    echo
    echo "处理完成! 成功: ${success}/${total}"
    [ "$success" -eq 0 ] && exit 1 || exit 0
}

# 执行
main
