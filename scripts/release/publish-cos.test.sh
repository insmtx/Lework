#!/usr/bin/env bash
# publish-cos.sh 的 dry-run 测试：只验证上传计划输出，不访问网络。
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
mkdir -p "$tmp/android" "$tmp/server"
printf apk > "$tmp/android/lework-android-0.8.0.apk"
printf tar > "$tmp/server/lework_0.8.0_linux_amd64.tar.gz"

# 稳定版本且现有 RELEASE.txt 更旧：应上传产物并同时更新 RELEASE.txt 与 LATEST.txt。
out="$(DRY_RUN=1 UPDATE_RELEASE_FILE=1 RELEASE_TXT_EXISTING=0.7.9 \
  RELEASE_PREFIX=application \
  bash "$script_dir/publish-cos.sh" "$tmp" 0.8.0)"

printf '%s' "$out" | grep -q "coscmd upload"
printf '%s' "$out" | grep -q "application/0.8.0/android/lework-android-0.8.0.apk"
printf '%s' "$out" | grep -q "application/0.8.0/server/lework_0.8.0_linux_amd64.tar.gz"
printf '%s' "$out" | grep -q "application/RELEASE.txt"
printf '%s' "$out" | grep -q "application/LATEST.txt"

# 首次统一发布：RELEASE.txt 不存在时回落到旧 LATEST.txt 判断，版本更大仍应更新。
out="$(DRY_RUN=1 UPDATE_RELEASE_FILE=1 RELEASE_TXT_EXISTING= LATEST_TXT_EXISTING=0.6.1 \
  RELEASE_PREFIX=application \
  bash "$script_dir/publish-cos.sh" "$tmp" 0.8.0)"
printf '%s' "$out" | grep -q "application/RELEASE.txt"
printf '%s' "$out" | grep -q "application/LATEST.txt"

# 现有 RELEASE.txt 已等于新版本：不得再更新任何指针。
out="$(DRY_RUN=1 UPDATE_RELEASE_FILE=1 RELEASE_TXT_EXISTING=0.8.0 \
  RELEASE_PREFIX=application \
  bash "$script_dir/publish-cos.sh" "$tmp" 0.8.0)"
if printf '%s' "$out" | grep -Eq "application/(RELEASE|LATEST)\.txt"; then
  echo "期望：现有版本等于新版本时不更新指针文件" >&2
  exit 1
fi

# 旧 LATEST.txt 比新版本更大：不得回退指针。
out="$(DRY_RUN=1 UPDATE_RELEASE_FILE=1 RELEASE_TXT_EXISTING= LATEST_TXT_EXISTING=0.9.0 \
  RELEASE_PREFIX=application \
  bash "$script_dir/publish-cos.sh" "$tmp" 0.8.0)"
if printf '%s' "$out" | grep -Eq "application/(RELEASE|LATEST)\.txt"; then
  echo "期望：新版本小于现有 LATEST.txt 时不更新指针文件" >&2
  exit 1
fi

# 预发布版本（作为第二个参数传入）：不得更新任何指针。
out="$(DRY_RUN=1 UPDATE_RELEASE_FILE=1 RELEASE_TXT_EXISTING=0.7.9 \
  RELEASE_PREFIX=application \
  bash "$script_dir/publish-cos.sh" "$tmp" 0.8.0-rc.1)"
if printf '%s' "$out" | grep -Eq "application/(RELEASE|LATEST)\.txt"; then
  echo "期望：预发布版本不更新指针文件" >&2
  exit 1
fi

# UPDATE_RELEASE_FILE 未开启时：不得更新任何指针。
out="$(DRY_RUN=1 RELEASE_PREFIX=application \
  bash "$script_dir/publish-cos.sh" "$tmp" 0.8.0)"
if printf '%s' "$out" | grep -Eq "application/(RELEASE|LATEST)\.txt"; then
  echo "期望：UPDATE_RELEASE_FILE 未开启时不更新指针文件" >&2
  exit 1
fi

echo "publish-cos.test.sh PASS"
