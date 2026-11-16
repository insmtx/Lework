#!/usr/bin/env bash
# publish-cos.sh 的 dry-run 测试：只验证上传计划输出，不访问网络。
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
mkdir -p "$tmp/android" "$tmp/server" "$tmp/root"
printf apk > "$tmp/android/lework-android-0.8.0.apk"
printf tar > "$tmp/server/lework_0.8.0_linux_amd64.tar.gz"
printf dmg > "$tmp/root/Lework-0.8.0-mac-arm64.dmg"

# 稳定版本且现有 LATEST.txt 更旧：应上传产物、根目录安装包并推进 LATEST.txt。
out="$(DRY_RUN=1 UPDATE_LATEST_FILE=1 LATEST_TXT_EXISTING=0.7.9 ROOT_INSTALLER_DIR="$tmp/root" \
  RELEASE_PREFIX=application \
  bash "$script_dir/publish-cos.sh" "$tmp" 0.8.0)"

printf '%s' "$out" | grep -q "coscmd upload"
printf '%s' "$out" | grep -q "application/0.8.0/android/lework-android-0.8.0.apk"
printf '%s' "$out" | grep -q "application/0.8.0/server/lework_0.8.0_linux_amd64.tar.gz"
printf '%s' "$out" | grep -q "application/Lework-0.8.0-mac-arm64.dmg"
printf '%s' "$out" | grep -q "application/LATEST.txt"

# 没有现有指针：应直接写入 LATEST.txt。
out="$(DRY_RUN=1 UPDATE_LATEST_FILE=1 LATEST_TXT_EXISTING= \
  RELEASE_PREFIX=application \
  bash "$script_dir/publish-cos.sh" "$tmp" 0.8.0)"
printf '%s' "$out" | grep -q "application/LATEST.txt"

# 现有 LATEST.txt 已等于新版本：不得更新指针。
out="$(DRY_RUN=1 UPDATE_LATEST_FILE=1 LATEST_TXT_EXISTING=0.8.0 \
  RELEASE_PREFIX=application \
  bash "$script_dir/publish-cos.sh" "$tmp" 0.8.0)"
if printf '%s' "$out" | grep -q "application/LATEST.txt"; then
  echo "期望：现有版本等于新版本时不更新 LATEST.txt" >&2
  exit 1
fi

# 现有 LATEST.txt 比新版本更大：不得回退指针。
out="$(DRY_RUN=1 UPDATE_LATEST_FILE=1 LATEST_TXT_EXISTING=0.9.0 \
  RELEASE_PREFIX=application \
  bash "$script_dir/publish-cos.sh" "$tmp" 0.8.0)"
if printf '%s' "$out" | grep -q "application/LATEST.txt"; then
  echo "期望：新版本小于现有 LATEST.txt 时不更新指针" >&2
  exit 1
fi

# 预发布版本：不得更新 LATEST.txt。
out="$(DRY_RUN=1 UPDATE_LATEST_FILE=1 LATEST_TXT_EXISTING=0.7.9 \
  RELEASE_PREFIX=application \
  bash "$script_dir/publish-cos.sh" "$tmp" 0.8.0-rc.1)"
if printf '%s' "$out" | grep -q "application/LATEST.txt"; then
  echo "期望：预发布版本不更新 LATEST.txt" >&2
  exit 1
fi

# UPDATE_LATEST_FILE 未开启时：不得更新 LATEST.txt。
out="$(DRY_RUN=1 RELEASE_PREFIX=application \
  bash "$script_dir/publish-cos.sh" "$tmp" 0.8.0)"
if printf '%s' "$out" | grep -q "application/LATEST.txt"; then
  echo "期望：UPDATE_LATEST_FILE 未开启时不更新 LATEST.txt" >&2
  exit 1
fi

echo "publish-cos.test.sh PASS"
