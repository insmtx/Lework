#!/usr/bin/env bash
# 将本地版本目录按统一发布布局上传到 COS 的 ${RELEASE_PREFIX}/<version>/ 下，
# 并可选把版本指针更新为最新稳定版本（仅稳定版本且新版本更大时更新）。
#
# 版本指针写两份：RELEASE.txt 是统一发布的新指针，LATEST.txt 沿用旧下载页逻辑，
# 两者内容相同（单行版本号），保证外部下载页和客户端都能读到同一个版本。
#
# 用法：publish-cos.sh <local-version-dir> <version>
# 环境变量：
#   RELEASE_PREFIX        COS 路径前缀，默认 application
#   PUBLIC_BASE           公开访问域名，用于读取现有指针文件
#   COS_SECRET_ID/KEY/BUCKET/REGION  coscmd 凭据（非 dry-run 必填）
#   UPDATE_RELEASE_FILE   1 时尝试更新指针文件，默认 0
#   DRY_RUN               1 时只打印 coscmd upload 命令，不访问网络
#   RELEASE_TXT_EXISTING  测试用：dry-run 下模拟 COS 上现有的 RELEASE.txt 内容
#   LATEST_TXT_EXISTING   测试用：dry-run 下模拟 COS 上现有的 LATEST.txt 内容
set -euo pipefail

if [[ $# -ne 2 ]]; then
  echo "用法：$0 <local-version-dir> <version>" >&2
  exit 1
fi

local_dir="${1%/}"
version="$2"

if [[ ! -d "$local_dir" ]]; then
  echo "本地版本目录不存在：$local_dir" >&2
  exit 1
fi

RELEASE_PREFIX="${RELEASE_PREFIX:-application}"
UPDATE_RELEASE_FILE="${UPDATE_RELEASE_FILE:-0}"
DRY_RUN="${DRY_RUN:-0}"
PUBLIC_BASE="${PUBLIC_BASE:-https://leros-1395325824.cos.ap-beijing.myqcloud.com}"

# version.mjs 与本脚本同目录，按脚本位置解析，不依赖调用时的当前目录。
script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
version_cli="${script_dir}/version.mjs"

# 上传单个文件；dry-run 只打印将要执行的命令。
upload() {
  local src="$1"
  local dest="$2"
  if [[ "$DRY_RUN" == "1" ]]; then
    printf 'coscmd upload %s %s\n' "$src" "$dest"
  else
    coscmd upload "$src" "$dest"
  fi
}

if [[ "$DRY_RUN" != "1" ]]; then
  if [[ -z "${COS_SECRET_ID:-}" || -z "${COS_SECRET_KEY:-}" || -z "${COS_BUCKET:-}" || -z "${COS_REGION:-}" ]]; then
    echo "缺少 COS 凭据：需要 COS_SECRET_ID / COS_SECRET_KEY / COS_BUCKET / COS_REGION" >&2
    exit 1
  fi
  coscmd config -a "$COS_SECRET_ID" -s "$COS_SECRET_KEY" -b "$COS_BUCKET" -r "$COS_REGION"
fi

# 遍历本地目录下的全部文件，按相对路径上传到 ${RELEASE_PREFIX}/<version>/ 下。
while IFS= read -r -d '' file; do
  rel="${file#"$local_dir"/}"
  upload "$file" "${RELEASE_PREFIX}/${version}/${rel}"
done < <(find "$local_dir" -type f -print0 | sort -z)

# 仅稳定版本且开启开关时才考虑更新版本指针。
if [[ "$UPDATE_RELEASE_FILE" == "1" && "$(node "$version_cli" is-stable "$version")" == "true" ]]; then
  read_pointer() {
    local name="$1"
    local existing_var="$2"
    if [[ "$DRY_RUN" == "1" ]]; then
      printf '%s' "${!existing_var:-}"
    else
      # 404（尚无任何发布）视为空，允许直接写入。
      curl -fsS "${PUBLIC_BASE}/${RELEASE_PREFIX}/${name}?t=$(date +%s)" || true
    fi
  }

  # 首次统一发布时 RELEASE.txt 还不存在，回落到旧 LATEST.txt 判断，避免版本指针回退。
  existing="$(read_pointer RELEASE.txt RELEASE_TXT_EXISTING | tr -d '[:space:]')"
  if [[ -z "$existing" ]]; then
    existing="$(read_pointer LATEST.txt LATEST_TXT_EXISTING | tr -d '[:space:]')"
  fi

  # 指针只允许单调前进：无现有版本或新版本更大时才上传。
  if [[ -z "$existing" || "$(node "$version_cli" gt "$version" "$existing")" == "true" ]]; then
    pointer_txt="$(mktemp)"
    trap 'rm -f "$pointer_txt"' EXIT
    printf '%s\n' "$version" > "$pointer_txt"
    upload "$pointer_txt" "${RELEASE_PREFIX}/RELEASE.txt"
    upload "$pointer_txt" "${RELEASE_PREFIX}/LATEST.txt"
  fi
fi
