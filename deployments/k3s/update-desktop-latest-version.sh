#!/usr/bin/env bash
set -euo pipefail

usage() {
  echo "Usage: $0 <desktop-latest-version>" >&2
}

if [ "$#" -ne 1 ]; then
  usage
  exit 2
fi

desktop_latest_version="$1"

namespace="${K3S_NAMESPACE:-leros}"
server_deployment="${K3S_SERVER_DEPLOYMENT:-leros}"
server_configmap="${K3S_SERVER_CONFIGMAP:-leros-server-config}"
server_config_key="${K3S_SERVER_CONFIG_KEY:-config.yaml}"

require_command() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "Required command is missing: $1" >&2
    exit 1
  fi
}

update_desktop_latest_version() {
  local input_file="$1"
  local output_file="$2"

  awk -v version="$desktop_latest_version" '
    function emit_client_update_block() {
      print "client_update:"
      print "  desktop:"
      print "    latest_version: \"" version "\""
      saw_client_update = 1
      updated = 1
    }

    function emit_desktop_block() {
      print "  desktop:"
      print "    latest_version: \"" version "\""
      saw_desktop = 1
      updated = 1
    }

    function emit_latest_version() {
      print "    latest_version: \"" version "\""
      updated = 1
    }

    BEGIN {
      in_client_update = 0
      in_desktop = 0
      saw_client_update = 0
      saw_desktop = 0
      updated = 0
    }

    /^[[:space:]]*client_update:[[:space:]]*$/ {
      print
      in_client_update = 1
      in_desktop = 0
      saw_client_update = 1
      next
    }

    in_client_update && /^[^[:space:]][^:]*:/ {
      if (!saw_desktop) {
        emit_desktop_block()
      } else if (in_desktop && !updated) {
        emit_latest_version()
      }
      in_client_update = 0
      in_desktop = 0
    }

    in_client_update && /^[[:space:]]{2}desktop:[[:space:]]*$/ {
      print
      in_desktop = 1
      saw_desktop = 1
      next
    }

    in_desktop && /^[[:space:]]{2}[^[:space:]][^:]*:/ {
      if (!updated) {
        emit_latest_version()
      }
      in_desktop = 0
    }

    in_desktop && /^[[:space:]]*latest_version:[[:space:]]*/ {
      emit_latest_version()
      next
    }

    { print }

    END {
      if (!saw_client_update) {
        emit_client_update_block()
      } else if (!saw_desktop) {
        emit_desktop_block()
      } else if (in_desktop && !updated) {
        emit_latest_version()
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

update_desktop_latest_version "$config_file" "$updated_config_file"

echo "Updating client_update.desktop.latest_version to ${desktop_latest_version}"
kubectl -n "$namespace" create configmap "$server_configmap" \
  "--from-file=${server_config_key}=${updated_config_file}" \
  --dry-run=client \
  -o yaml | kubectl apply -f -

echo "Restarting server deployment so it reloads ConfigMap"
kubectl -n "$namespace" rollout restart "deployment/${server_deployment}"
kubectl -n "$namespace" rollout status "deployment/${server_deployment}" --timeout=180s

echo "Desktop latest version: ${desktop_latest_version}"
