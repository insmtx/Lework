#!/bin/sh
# 容器内 kubectl 不会自动使用 ServiceAccount 凭据，这里生成只含只读 SA 的 kubeconfig。
set -eu

SA_DIR=/var/run/secrets/kubernetes.io/serviceaccount
if [ -z "${SPARK_KUBECTL_ARGS:-}" ] && [ -f "$SA_DIR/token" ] && [ -n "${KUBERNETES_SERVICE_HOST:-}" ]; then
    cat > /tmp/kubeconfig <<EOF
apiVersion: v1
kind: Config
clusters:
  - name: in-cluster
    cluster:
      certificate-authority: $SA_DIR/ca.crt
      server: https://$KUBERNETES_SERVICE_HOST:$KUBERNETES_SERVICE_PORT
users:
  - name: serviceaccount
    user:
      tokenFile: $SA_DIR/token
contexts:
  - name: in-cluster
    context:
      cluster: in-cluster
      user: serviceaccount
current-context: in-cluster
EOF
    export SPARK_KUBECTL_ARGS="--kubeconfig=/tmp/kubeconfig"
fi

exec python3 /usr/local/lib/leros-spark/power_agent.py "$@"
