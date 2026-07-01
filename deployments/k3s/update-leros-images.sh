#!/usr/bin/env bash
set -euo pipefail

usage() {
  echo "Usage: $0 <server-image> <worker-image>" >&2
}

if [ "$#" -ne 2 ]; then
  usage
  exit 2
fi

server_image="$1"
worker_image="$2"

namespace="${K3S_NAMESPACE:-leros}"
server_deployment="${K3S_SERVER_DEPLOYMENT:-leros}"
server_container="${K3S_SERVER_CONTAINER:-leros}"
server_configmap="${K3S_SERVER_CONFIGMAP:-leros-server-config}"
server_config_key="${K3S_SERVER_CONFIG_KEY:-config.yaml}"

require_command() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "Required command is missing: $1" >&2
    exit 1
  fi
}

update_worker_image() {
  local input_file="$1"
  local output_file="$2"

  awk -v image="$worker_image" '
    BEGIN {
      in_scheduler = 0
      updated = 0
      saw_scheduler = 0
    }
    /^[[:space:]]*scheduler:[[:space:]]*$/ {
      print
      in_scheduler = 1
      saw_scheduler = 1
      next
    }
    in_scheduler && /^[^[:space:]][^:]*:/ {
      if (!updated) {
        print "  worker_image: \"" image "\""
        updated = 1
      }
      in_scheduler = 0
    }
    in_scheduler && /^[[:space:]]*worker_image:[[:space:]]*/ {
      print "  worker_image: \"" image "\""
      updated = 1
      next
    }
    { print }
    END {
      if (in_scheduler && !updated) {
        print "  worker_image: \"" image "\""
        updated = 1
      }
      if (!saw_scheduler) {
        print "scheduler:"
        print "  worker_image: \"" image "\""
      }
    }
  ' "$input_file" > "$output_file"
}

require_command kubectl
require_command awk

work_dir="$(mktemp -d)"
trap 'rm -rf "$work_dir"' EXIT

config_file="$work_dir/config.yaml"
updated_config_file="$work_dir/config.updated.yaml"

echo "Reading ConfigMap ${namespace}/${server_configmap}:${server_config_key}"
kubectl -n "$namespace" get configmap "$server_configmap" \
  -o "go-template={{ index .data \"${server_config_key}\" }}" > "$config_file"

update_worker_image "$config_file" "$updated_config_file"

echo "Updating scheduler.worker_image to ${worker_image}"
kubectl -n "$namespace" create configmap "$server_configmap" \
  "--from-file=${server_config_key}=${updated_config_file}" \
  --dry-run=client \
  -o yaml | kubectl apply -f -

echo "Updating server image to ${server_image}"
kubectl -n "$namespace" set image "deployment/${server_deployment}" "${server_container}=${server_image}"

echo "Restarting server deployment so it reloads ConfigMap"
kubectl -n "$namespace" rollout restart "deployment/${server_deployment}"
kubectl -n "$namespace" rollout status "deployment/${server_deployment}" --timeout=180s

echo "Server desired image: ${server_image}"
echo "Worker desired image: ${worker_image}"
