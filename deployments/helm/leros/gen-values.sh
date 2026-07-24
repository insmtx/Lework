#!/usr/bin/env bash
set -euo pipefail

# =============================================================================
# 从 values.yaml.template 生成 values.yaml（含随机密钥）
# =============================================================================
# 占位符说明：
#   @PG_PASSWORD@         随机生成 16 位
#   @NATS_PASSWORD@       随机生成 16 位
#   @JWT_SECRET@          随机生成 32 位
#   @STORAGE_SIGN_SECRET@ 随机生成 32 位
#   @REGISTRY@            --registry 传入，不传则留空
#   @REGISTRY_USER@       --user 传入
#   @REGISTRY_PASS@       --pass 传入
# llm.apiKey 不自动生成，需手动填写。
#
# 用法：
#   ./gen-values.sh                                        # 生成 values.yaml
#   ./gen-values.sh --force                                # 强制覆盖已有文件
#   ./gen-values.sh -f prod.yaml                           # 指定输出文件名
#   ./gen-values.sh --registry registry.yygu.cn \
#                  --user myuser --pass mypass            # 同时填入镜像仓库凭证
# =============================================================================

BASE_DIR=$(cd "$(dirname "$0")" && pwd)
cd "$BASE_DIR"
echo "已切换到脚本目录: $(pwd)"

# -------------------------------
# 参数解析
# -------------------------------
OUTPUT_FILE="values.yaml"
TEMPLATE_FILE="values.yaml.template"
FORCE=false
REGISTRY=""
REGISTRY_USER=""
REGISTRY_PASS=""

while [[ $# -gt 0 ]]; do
  case "$1" in
    -f|--output)      OUTPUT_FILE="$2"; shift 2 ;;
    -t|--template)    TEMPLATE_FILE="$2"; shift 2 ;;
    --force)          FORCE=true; shift ;;
    --registry)       REGISTRY="$2"; shift 2 ;;
    --user)           REGISTRY_USER="$2"; shift 2 ;;
    --pass)           REGISTRY_PASS="$2"; shift 2 ;;
    -h|--help)        sed -n '3,18p' "$0"; exit 0 ;;
    *)                echo "未知参数: $1"; exit 1 ;;
  esac
done

# -------------------------------
# 前置检查
# -------------------------------
if [ -f "$OUTPUT_FILE" ] && [ "$FORCE" != "true" ]; then
  echo "[skip] ${OUTPUT_FILE} 已存在，跳过生成（使用 --force 可强制覆盖）"
  exit 0
fi

if [ ! -f "$TEMPLATE_FILE" ]; then
  echo "[error] 找不到模板文件 ${TEMPLATE_FILE}"
  exit 1
fi

# -------------------------------
# 生成随机密钥
# -------------------------------
gen_password() { openssl rand -base64 48 | tr -dc A-Za-z0-9 | head -c 16; }
gen_secret()   { openssl rand -base64 48 | tr -dc A-Za-z0-9 | head -c 32; }

PG_PASSWORD=$(gen_password)
NATS_PASSWORD=$(gen_password)
JWT_SECRET=$(gen_secret)
STORAGE_SIGN_SECRET=$(gen_secret)

echo ""
echo "[keys] 生成随机密钥："
echo "   PostgreSQL Password : ${PG_PASSWORD}"
echo "   NATS Password       : ${NATS_PASSWORD}"
echo "   JWT Secret          : ${JWT_SECRET}"
echo "   Storage Sign Secret : ${STORAGE_SIGN_SECRET}"
echo ""

# -------------------------------
# 从模板生成 values.yaml（占位符替换）
# -------------------------------
cp "$TEMPLATE_FILE" "$OUTPUT_FILE"

sed_inplace() {
  if sed --version >/dev/null 2>&1; then sed -i "$@"; else sed -i '' "$@"; fi
}

sed_inplace \
  -e "s|@PG_PASSWORD@|${PG_PASSWORD}|g" \
  -e "s|@NATS_PASSWORD@|${NATS_PASSWORD}|g" \
  -e "s|@JWT_SECRET@|${JWT_SECRET}|g" \
  -e "s|@STORAGE_SIGN_SECRET@|${STORAGE_SIGN_SECRET}|g" \
  -e "s|@REGISTRY@|${REGISTRY}|g" \
  -e "s|@REGISTRY_USER@|${REGISTRY_USER}|g" \
  -e "s|@REGISTRY_PASS@|${REGISTRY_PASS}|g" \
  "$OUTPUT_FILE"

# -------------------------------
# 输出结果
# -------------------------------
echo "[done] ${OUTPUT_FILE} 已生成"
echo ""
echo "[todo] 请手动修改以下配置后部署："
echo "   - llm.apiKey / llm.baseUrl / llm.model   # LLM 模型配置（API Key 必填，外挂自有凭证）"
echo "   - server.image / worker.image            # 镜像 tag"
echo "   - *.nodeSelector                         # 钉到同一节点（hostPath 共享数据）"
echo "   - ingress.enabled / ingress.server.host   # 有域名时开启 Ingress"
echo ""
echo "   部署命令："
echo "   helm install leros ./deployments/helm/leros -n leros --create-namespace -f ${OUTPUT_FILE}"
