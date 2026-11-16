#!/usr/bin/env bash
# 将本地版本目录按统一发布布局上传到 COS 的 ${RELEASE_PREFIX}/<version>/ 下，
# 并在稳定版本时把 LATEST.txt 更新为最新版本号。
#
# LATEST.txt 沿用旧下载页的指针文件与内容格式（单行版本号），桌面端、移动端和
# 外部下载页都读同一个文件，避免维护两套指针。
#
# 用法：publish-cos.sh <local-version-dir> <version>
# 环境变量：
#   RELEASE_PREFIX        COS 路径前缀，默认 application
#   PUBLIC_BASE           公开访问域名，用于读取现有 LATEST.txt
#   COS_SECRET_ID/KEY/BUCKET  coscmd 凭据（非 dry-run 必填）
#   COS_REGION            地域，未设置 COS_ENDPOINT 时使用（非 dry-run 必填）
#   COS_ENDPOINT          自定义 endpoint 主机名（不含协议与 bucket），如
#                         cos.accelerate.myqcloud.com；设置后不再使用地域
#   COS_RETRY / COS_TIMEOUT  单次请求重试次数与超时秒数，默认 5 / 120
#   UPDATE_LATEST_FILE    1 时尝试更新 LATEST.txt，默认 0
#   ROOT_INSTALLER_DIR    非空时把该目录下的安装包平铺上传到 ${RELEASE_PREFIX}/ 根目录
#   DRY_RUN               1 时只打印 coscmd upload 命令，不访问网络
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
UPDATE_LATEST_FILE="${UPDATE_LATEST_FILE:-0}"
ROOT_INSTALLER_DIR="${ROOT_INSTALLER_DIR:-}"
DRY_RUN="${DRY_RUN:-0}"
PUBLIC_BASE="${PUBLIC_BASE:-https://leros-1395325824.cos.ap-beijing.myqcloud.com}"

# version.mjs 与本脚本同目录，按脚本位置解析，不依赖调用时的当前目录。
script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
version_cli="${script_dir}/version.mjs"

# 上传单个文件；dry-run 只打印将要执行的命令。
# 海外 runner 直连 ap-beijing 常见超时，coscmd 自带的分片重试不足以覆盖整文件失败，
# 这里再包一层整文件重试。
upload() {
  local src="$1"
  local dest="$2"
  if [[ "$DRY_RUN" == "1" ]]; then
    printf 'coscmd upload %s %s\n' "$src" "$dest"
    return 0
  fi

  local attempt
  for attempt in 1 2 3; do
    if coscmd upload "$src" "$dest"; then
      return 0
    fi
    echo "上传失败，准备第 ${attempt} 次重试：${src}" >&2
    sleep $((attempt * 5))
  done

  return 1
}

# endpoint 与 region 在 coscmd 里互斥：设置 COS_ENDPOINT 时用它，否则回落到地域。
endpoint_args=()
if [[ -n "${COS_ENDPOINT:-}" ]]; then
  endpoint_args=(-e "$COS_ENDPOINT")
elif [[ -n "${COS_REGION:-}" ]]; then
  endpoint_args=(-r "$COS_REGION")
fi

if [[ "$DRY_RUN" == "1" ]]; then
  printf 'coscmd config -a *** -s *** -b %s %s --retry %s --timeout %s\n' \
    "${COS_BUCKET:-<bucket>}" "${endpoint_args[*]}" "${COS_RETRY:-5}" "${COS_TIMEOUT:-120}"
else
  if [[ -z "${COS_SECRET_ID:-}" || -z "${COS_SECRET_KEY:-}" || -z "${COS_BUCKET:-}" || ${#endpoint_args[@]} -eq 0 ]]; then
    echo "缺少 COS 凭据：需要 COS_SECRET_ID / COS_SECRET_KEY / COS_BUCKET，以及 COS_ENDPOINT 或 COS_REGION" >&2
    exit 1
  fi
  coscmd config -a "$COS_SECRET_ID" -s "$COS_SECRET_KEY" -b "$COS_BUCKET" \
    "${endpoint_args[@]}" --retry "${COS_RETRY:-5}" --timeout "${COS_TIMEOUT:-120}"
fi

# 遍历本地目录下的全部文件，按相对路径上传到 ${RELEASE_PREFIX}/<version>/ 下。
while IFS= read -r -d '' file; do
  rel="${file#"$local_dir"/}"
  upload "$file" "${RELEASE_PREFIX}/${version}/${rel}"
done < <(find "$local_dir" -type f -print0 | sort -z)

# 旧下载页按根目录文件名下载安装包，这里把安装包平铺到 ${RELEASE_PREFIX}/ 根目录。
if [[ -n "$ROOT_INSTALLER_DIR" ]]; then
  if [[ ! -d "$ROOT_INSTALLER_DIR" ]]; then
    echo "根目录安装包目录不存在：$ROOT_INSTALLER_DIR" >&2
    exit 1
  fi
  while IFS= read -r -d '' file; do
    upload "$file" "${RELEASE_PREFIX}/$(basename "$file")"
  done < <(find "$ROOT_INSTALLER_DIR" -maxdepth 1 -type f -print0 | sort -z)
fi

# 仅稳定版本且开启开关时才考虑推进 LATEST.txt。
if [[ "$UPDATE_LATEST_FILE" == "1" && "$(node "$version_cli" is-stable "$version")" == "true" ]]; then
  if [[ "$DRY_RUN" == "1" ]]; then
    existing="${LATEST_TXT_EXISTING:-}"
  else
    # 404（尚无任何发布）视为空，允许直接写入；其它失败必须中止，
    # 否则一次网络超时会被当成“没有现有版本”而把指针回退。
    latest_body="$(mktemp)"
    status="$(curl -sS --max-time 60 -o "$latest_body" -w '%{http_code}' \
      "${PUBLIC_BASE}/${RELEASE_PREFIX}/LATEST.txt?t=$(date +%s)" || echo "000")"
    case "$status" in
      200) existing="$(cat "$latest_body")" ;;
      404) existing="" ;;
      *)
        rm -f "$latest_body"
        echo "读取现有 LATEST.txt 失败（HTTP ${status}），中止以免回退版本指针" >&2
        exit 1
        ;;
    esac
    rm -f "$latest_body"
  fi
  existing="$(printf '%s' "$existing" | tr -d '[:space:]')"

  # LATEST.txt 只允许单调前进：无现有版本或新版本更大时才上传。
  if [[ -z "$existing" || "$(node "$version_cli" gt "$version" "$existing")" == "true" ]]; then
    latest_txt="$(mktemp)"
    trap 'rm -f "$latest_txt"' EXIT
    printf '%s\n' "$version" > "$latest_txt"
    upload "$latest_txt" "${RELEASE_PREFIX}/LATEST.txt"
  fi
fi
