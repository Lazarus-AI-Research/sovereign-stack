#!/usr/bin/env bash
# Shared container-engine selection and invocation for installer and lifecycle
# commands. Callers must set SOVEREIGN_HOME before loading provider state.

sovereign_engine_error() {
  printf 'error: %s\n' "$*" >&2
  return 1
}

sovereign_engine_state_file() {
  printf '%s/state/container-engine.env' "${SOVEREIGN_HOME:?SOVEREIGN_HOME is required}"
}

sovereign_engine_state_value() {
  local file="$1" key="$2"
  awk -v key="$key" '
    index($0, key "=") == 1 {
      value = substr($0, length(key) + 2)
    }
    END { print value }
  ' "$file"
}

sovereign_engine_safe_value() {
  [[ "$1" != *$'\n'* && "$1" != *$'\r'* ]]
}

sovereign_engine_write_state() {
  local provider="$1" managed="$2" docker_cli="$3" docker_context="$4"
  local docker_config="${5:-}" colima_cli="${6:-}" colima_home="${7:-}"
  local colima_profile="${8:-}" tool_bin="${9:-}" file temporary value
  for value in "$provider" "$managed" "$docker_cli" "$docker_context" \
    "$docker_config" "$colima_cli" "$colima_home" "$colima_profile" "$tool_bin"; do
    sovereign_engine_safe_value "$value" || return 1
  done
  file="$(sovereign_engine_state_file)"
  mkdir -p "$(dirname "$file")"
  temporary="$file.tmp.$$"
  umask 077
  {
    printf 'schema_version=1\n'
    printf 'provider=%s\n' "$provider"
    printf 'managed=%s\n' "$managed"
    printf 'docker_cli=%s\n' "$docker_cli"
    printf 'docker_context=%s\n' "$docker_context"
    printf 'docker_config=%s\n' "$docker_config"
    printf 'colima_cli=%s\n' "$colima_cli"
    printf 'colima_home=%s\n' "$colima_home"
    printf 'colima_profile=%s\n' "$colima_profile"
    printf 'tool_bin=%s\n' "$tool_bin"
  } > "$temporary"
  chmod 600 "$temporary"
  mv "$temporary" "$file"
}

sovereign_engine_load() {
  local file schema
  file="$(sovereign_engine_state_file)"
  [[ -r "$file" ]] || return 1
  schema="$(sovereign_engine_state_value "$file" schema_version)"
  [[ "$schema" == 1 ]] || return 1
  SOVEREIGN_ENGINE_PROVIDER="$(sovereign_engine_state_value "$file" provider)"
  SOVEREIGN_ENGINE_MANAGED="$(sovereign_engine_state_value "$file" managed)"
  SOVEREIGN_ENGINE_DOCKER_CLI="$(sovereign_engine_state_value "$file" docker_cli)"
  SOVEREIGN_ENGINE_DOCKER_CONTEXT="$(sovereign_engine_state_value "$file" docker_context)"
  SOVEREIGN_ENGINE_DOCKER_CONFIG="$(sovereign_engine_state_value "$file" docker_config)"
  SOVEREIGN_ENGINE_COLIMA_CLI="$(sovereign_engine_state_value "$file" colima_cli)"
  SOVEREIGN_ENGINE_COLIMA_HOME="$(sovereign_engine_state_value "$file" colima_home)"
  SOVEREIGN_ENGINE_COLIMA_PROFILE="$(sovereign_engine_state_value "$file" colima_profile)"
  SOVEREIGN_ENGINE_TOOL_BIN="$(sovereign_engine_state_value "$file" tool_bin)"
  case "$SOVEREIGN_ENGINE_PROVIDER" in
    existing|managed-colima|managed-docker) ;;
    *) return 1 ;;
  esac
  [[ "$SOVEREIGN_ENGINE_MANAGED" == 0 || "$SOVEREIGN_ENGINE_MANAGED" == 1 ]] || return 1
  [[ -x "$SOVEREIGN_ENGINE_DOCKER_CLI" ]] || return 1
  [[ -z "$SOVEREIGN_ENGINE_DOCKER_CONTEXT" ||
     "$SOVEREIGN_ENGINE_DOCKER_CONTEXT" =~ ^[A-Za-z0-9][A-Za-z0-9_.-]*$ ]] || return 1
  SOVEREIGN_ENGINE_LOADED=1
}

sovereign_engine_docker() {
  [[ "${SOVEREIGN_ENGINE_LOADED:-0}" == 1 ]] || sovereign_engine_load || {
    sovereign_engine_error "container-engine state is missing or invalid; run sovereign repair"
    return 1
  }
  if [[ -n "$SOVEREIGN_ENGINE_DOCKER_CONFIG" && -n "$SOVEREIGN_ENGINE_DOCKER_CONTEXT" ]]; then
    env DOCKER_CONFIG="$SOVEREIGN_ENGINE_DOCKER_CONFIG" \
      "$SOVEREIGN_ENGINE_DOCKER_CLI" --context "$SOVEREIGN_ENGINE_DOCKER_CONTEXT" "$@"
  elif [[ -n "$SOVEREIGN_ENGINE_DOCKER_CONFIG" ]]; then
    env DOCKER_CONFIG="$SOVEREIGN_ENGINE_DOCKER_CONFIG" \
      "$SOVEREIGN_ENGINE_DOCKER_CLI" "$@"
  elif [[ -n "$SOVEREIGN_ENGINE_DOCKER_CONTEXT" ]]; then
    "$SOVEREIGN_ENGINE_DOCKER_CLI" --context "$SOVEREIGN_ENGINE_DOCKER_CONTEXT" "$@"
  else
    "$SOVEREIGN_ENGINE_DOCKER_CLI" "$@"
  fi
}

sovereign_engine_compose() {
  sovereign_engine_docker compose "$@"
}

sovereign_engine_probe_basic() {
  sovereign_engine_docker info >/dev/null 2>&1 || return 1
  sovereign_engine_compose version >/dev/null 2>&1 || return 1
}

sovereign_engine_record_existing() {
  local docker_cli docker_context
  docker_cli="${SOVEREIGN_DOCKER_CLI:-$(command -v docker 2>/dev/null || true)}"
  [[ -n "$docker_cli" && -x "$docker_cli" ]] || return 1
  docker_context="$("$docker_cli" context show 2>/dev/null || true)"
  docker_context="${docker_context:-default}"
  [[ "$docker_context" =~ ^[A-Za-z0-9][A-Za-z0-9_.-]*$ ]] || return 1
  "$docker_cli" --context "$docker_context" info >/dev/null 2>&1 || return 1
  "$docker_cli" --context "$docker_context" compose version >/dev/null 2>&1 || return 1
  sovereign_engine_write_state existing 0 "$docker_cli" "$docker_context"
  sovereign_engine_load
}

sovereign_engine_sha256() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | awk '{print $1}'
  else
    shasum -a 256 "$1" | awk '{print $1}'
  fi
}

sovereign_engine_file_bytes() {
  if stat -f %z "$1" >/dev/null 2>&1; then stat -f %z "$1"
  else stat -c %s "$1"; fi
}

sovereign_engine_install_id() {
  if command -v sha256sum >/dev/null 2>&1; then
    printf '%s' "$SOVEREIGN_HOME" | sha256sum | awk '{print substr($1, 1, 12)}'
  else
    printf '%s' "$SOVEREIGN_HOME" | shasum -a 256 | awk '{print substr($1, 1, 12)}'
  fi
}

sovereign_engine_component_value() {
  local file="$1" component="$2" key="$3"
  awk -v component="$component" -v key="$key" '
    index($0, "\"" component "\"") && index($0, "{") { inside=1; next }
    inside && index($0, "\"" key "\"") {
      value=$0
      sub("^.*\"" key "\"[[:space:]]*:[[:space:]]*\"", "", value)
      sub("\".*$", "", value)
      print value
      exit
    }
    inside && $0 ~ /^[[:space:]]*},?[[:space:]]*$/ { exit }
  ' "$file"
}

sovereign_engine_component_number() {
  local file="$1" component="$2" key="$3"
  awk -v component="$component" -v key="$key" '
    index($0, "\"" component "\"") && index($0, "{") { inside=1; next }
    inside && index($0, "\"" key "\"") {
      value=$0
      sub("^.*\"" key "\"[[:space:]]*:[[:space:]]*", "", value)
      sub("[[:space:]]*,?[[:space:]]*$", "", value)
      print value
      exit
    }
    inside && $0 ~ /^[[:space:]]*},?[[:space:]]*$/ { exit }
  ' "$file"
}

sovereign_engine_verify_archive_paths() {
  local archive="$1" entry
  while IFS= read -r entry; do
    entry="${entry#./}"
    [[ -z "$entry" ]] && continue
    [[ "$entry" != /* && "/$entry/" != *"/../"* ]] || return 1
  done < <(tar -tzf "$archive")
}

sovereign_engine_download_component() {
  local manifest="$1" component="$2" destination="$3"
  local url artifact expected_hash expected_bytes actual_hash actual_bytes local_source
  url="$(sovereign_engine_component_value "$manifest" "$component" url)"
  artifact="$(sovereign_engine_component_value "$manifest" "$component" artifact)"
  expected_hash="$(sovereign_engine_component_value "$manifest" "$component" sha256)"
  expected_bytes="$(sovereign_engine_component_number "$manifest" "$component" bytes)"
  [[ "$url" == https://* || "$url" == file://* ]] || return 1
  [[ "$expected_hash" =~ ^[0-9a-f]{64}$ && "$expected_bytes" =~ ^[1-9][0-9]*$ ]] || return 1
  mkdir -p "$(dirname "$destination")"
  if [[ ! -f "$destination" || "$(sovereign_engine_sha256 "$destination")" != "$expected_hash" ]]; then
    rm -f "$destination"
    local_source="${SOVEREIGN_ENGINE_ARTIFACT_DIR:-}/$artifact"
    if [[ -n "${SOVEREIGN_ENGINE_ARTIFACT_DIR:-}" && -f "$local_source" ]]; then
      cp "$local_source" "$destination.part"
    else
      curl -fL --retry 4 --progress-bar -o "$destination.part" "$url" || {
        rm -f "$destination.part"
        return 1
      }
    fi
    actual_hash="$(sovereign_engine_sha256 "$destination.part")"
    actual_bytes="$(sovereign_engine_file_bytes "$destination.part")"
    [[ "$actual_hash" == "$expected_hash" && "$actual_bytes" == "$expected_bytes" ]] || {
      rm -f "$destination.part"
      return 1
    }
    mv "$destination.part" "$destination"
  fi
}

sovereign_engine_verify_component_signature() {
  local manifest="$1" component="$2" artifact="$3" signature_url signer cosign bundle local_bundle
  signature_url="$(sovereign_engine_component_value "$manifest" "$component" signature_url)"
  signer="$(sovereign_engine_component_value "$manifest" "$component" signer_identity_regexp)"
  [[ -n "$signature_url" ]] || return 0
  [[ "$signature_url" == https://* && -n "$signer" ]] || return 1
  cosign="${SOVEREIGN_COSIGN:-}"
  if [[ ! -x "$cosign" ]] && type install_cosign >/dev/null 2>&1; then
    install_cosign
    cosign="${TMP_ROOT:-}/cosign"
  fi
  [[ -x "$cosign" ]] || return 1
  bundle="$artifact.sigstore.json"
  if [[ ! -f "$bundle" ]]; then
    local_bundle="${SOVEREIGN_ENGINE_ARTIFACT_DIR:-}/$(basename "$signature_url")"
    if [[ -n "${SOVEREIGN_ENGINE_ARTIFACT_DIR:-}" && -f "$local_bundle" ]]; then
      cp "$local_bundle" "$bundle.part"
    else
      curl -fsSL --retry 4 -o "$bundle.part" "$signature_url" || return 1
    fi
    mv "$bundle.part" "$bundle"
  fi
  "$cosign" verify-blob --bundle "$bundle" \
    --certificate-identity-regexp "$signer" \
    --certificate-oidc-issuer https://token.actions.githubusercontent.com \
    "$artifact" >/dev/null
}

sovereign_engine_managed_colima_resources() {
  local host_cpus host_memory_gib cpus memory
  host_cpus="$(sysctl -n hw.ncpu 2>/dev/null || printf 4)"
  host_memory_gib="$(( $(sysctl -n hw.memsize 2>/dev/null || printf 34359738368) / 1024 / 1024 / 1024 ))"
  cpus=8
  (( host_cpus < cpus )) && cpus="$host_cpus"
  (( cpus < 4 )) && cpus=4
  memory=16
  (( host_memory_gib < 48 )) && memory=12
  printf '%s %s 100' "$cpus" "$memory"
}

sovereign_engine_provision_managed_colima() {
  local manifest="$1" component version artifact format toolchain_id tool_root staging downloads
  local colima_version lima_version docker_version compose_version archive docker_config colima_home
  local cpus memory disk
  [[ "${SOVEREIGN_ENGINE_PLATFORM:-$(uname -s)-$(uname -m)}" == Darwin-arm64 ]] || return 1
  for component in colima lima docker_cli docker_compose; do
    version="$(sovereign_engine_component_value "$manifest" "$component" version)"
    artifact="$(sovereign_engine_component_value "$manifest" "$component" artifact)"
    format="$(sovereign_engine_component_value "$manifest" "$component" format)"
    [[ "$version" =~ ^[A-Za-z0-9][A-Za-z0-9_.+-]*$ &&
       "$artifact" =~ ^[A-Za-z0-9][A-Za-z0-9_.+-]*$ ]] || return 1
    [[ "$format" == executable || "$format" == tar.gz ]] || return 1
  done
  colima_version="$(sovereign_engine_component_value "$manifest" colima version)"
  lima_version="$(sovereign_engine_component_value "$manifest" lima version)"
  docker_version="$(sovereign_engine_component_value "$manifest" docker_cli version)"
  compose_version="$(sovereign_engine_component_value "$manifest" docker_compose version)"
  toolchain_id="colima-$colima_version-lima-$lima_version-docker-$docker_version-compose-$compose_version"
  tool_root="$SOVEREIGN_HOME/tools/container/$toolchain_id"
  docker_config="$tool_root/docker-config"
  # Lima places several nested Unix sockets below its home. Keep this root
  # intentionally short so custom appliance paths cannot exceed macOS's
  # 104-byte sockaddr_un limit.
  colima_home="${SOVEREIGN_COLIMA_HOME:-$HOME/.sovereign-colima/$(sovereign_engine_install_id)}"
  if [[ ! -x "$tool_root/bin/colima" || ! -x "$tool_root/bin/limactl" ||
        ! -x "$tool_root/bin/docker" || ! -x "$docker_config/cli-plugins/docker-compose" ]]; then
    staging="$SOVEREIGN_HOME/tools/container/.$toolchain_id.tmp.$$"
    downloads="$SOVEREIGN_HOME/cache/installer-dependencies"
    rm -rf "$staging"
    mkdir -p "$staging/bin" "$staging/docker-config" "$downloads"

    artifact="$(sovereign_engine_component_value "$manifest" colima artifact)"
    sovereign_engine_download_component "$manifest" colima "$downloads/$artifact" || return 1
    install -m 755 "$downloads/$artifact" "$staging/bin/colima"

    artifact="$(sovereign_engine_component_value "$manifest" lima artifact)"
    sovereign_engine_download_component "$manifest" lima "$downloads/$artifact" || return 1
    sovereign_engine_verify_archive_paths "$downloads/$artifact" || return 1
    tar -xzf "$downloads/$artifact" -C "$staging"

    artifact="$(sovereign_engine_component_value "$manifest" docker_cli artifact)"
    sovereign_engine_download_component "$manifest" docker_cli "$downloads/$artifact" || return 1
    sovereign_engine_verify_archive_paths "$downloads/$artifact" || return 1
    mkdir -p "$staging/docker-unpack"
    tar -xzf "$downloads/$artifact" -C "$staging/docker-unpack"
    [[ -x "$staging/docker-unpack/docker/docker" ]] || return 1
    install -m 755 "$staging/docker-unpack/docker/docker" "$staging/bin/docker"

    artifact="$(sovereign_engine_component_value "$manifest" docker_compose artifact)"
    sovereign_engine_download_component "$manifest" docker_compose "$downloads/$artifact" || return 1
    sovereign_engine_verify_component_signature "$manifest" docker_compose "$downloads/$artifact" || return 1
    mkdir -p "$staging/docker-config/cli-plugins"
    install -m 755 "$downloads/$artifact" "$staging/docker-config/cli-plugins/docker-compose"

    rm -rf "$staging/docker-unpack"
    mkdir -p "$(dirname "$tool_root")"
    rm -rf "$tool_root"
    mv "$staging" "$tool_root"
  fi
  mkdir -p "$docker_config" "$colima_home" || return 1
  sovereign_engine_write_state managed-colima 1 "$tool_root/bin/docker" colima-sovereign \
    "$docker_config" "$tool_root/bin/colima" "$colima_home" sovereign "$tool_root/bin" || return 1
  unset SOVEREIGN_ENGINE_LOADED SOVEREIGN_ENGINE_READY
  sovereign_engine_load || return 1
  read -r cpus memory disk <<< "$(sovereign_engine_managed_colima_resources)"
  env COLIMA_HOME="$colima_home" DOCKER_CONFIG="$docker_config" \
    PATH="$tool_root/bin:/usr/bin:/bin:/usr/sbin:/sbin" \
    "$tool_root/bin/colima" start sovereign --activate=false --arch aarch64 \
      --vm-type vz --runtime docker --cpus "$cpus" --memory "$memory" --disk "$disk" \
      --mount "$SOVEREIGN_HOME:w" --mount-type virtiofs --ssh-config=false --save-config=true || return 1
}

sovereign_engine_start() {
  [[ "${SOVEREIGN_ENGINE_LOADED:-0}" == 1 ]] || sovereign_engine_load || return 1
  case "$SOVEREIGN_ENGINE_PROVIDER" in
    existing|managed-docker) return 0 ;;
    managed-colima)
      [[ -x "$SOVEREIGN_ENGINE_COLIMA_CLI" && -n "$SOVEREIGN_ENGINE_COLIMA_HOME" &&
         "$SOVEREIGN_ENGINE_COLIMA_PROFILE" =~ ^[A-Za-z0-9][A-Za-z0-9_.-]*$ ]] || return 1
      env COLIMA_HOME="$SOVEREIGN_ENGINE_COLIMA_HOME" \
        DOCKER_CONFIG="$SOVEREIGN_ENGINE_DOCKER_CONFIG" \
        PATH="$SOVEREIGN_ENGINE_TOOL_BIN:/usr/bin:/bin:/usr/sbin:/sbin" \
        "$SOVEREIGN_ENGINE_COLIMA_CLI" start "$SOVEREIGN_ENGINE_COLIMA_PROFILE" \
        --activate=false
      ;;
  esac
}

sovereign_engine_stop_managed() {
  [[ "${SOVEREIGN_ENGINE_LOADED:-0}" == 1 ]] || sovereign_engine_load || return 0
  [[ "$SOVEREIGN_ENGINE_PROVIDER" == managed-colima ]] || return 0
  env COLIMA_HOME="$SOVEREIGN_ENGINE_COLIMA_HOME" \
    PATH="$SOVEREIGN_ENGINE_TOOL_BIN:/usr/bin:/bin:/usr/sbin:/sbin" \
    "$SOVEREIGN_ENGINE_COLIMA_CLI" stop "$SOVEREIGN_ENGINE_COLIMA_PROFILE"
}

sovereign_engine_require() {
  [[ "${SOVEREIGN_ENGINE_READY:-0}" == 1 ]] && return 0
  if ! sovereign_engine_load; then
    if [[ "${SOVEREIGN_ENGINE_PREFER_MANAGED:-0}" != 1 ]] && sovereign_engine_record_existing; then
      :
    elif [[ "${SOVEREIGN_ENGINE_PLATFORM:-$(uname -s)-$(uname -m)}" == Darwin-arm64 &&
            -r "${SOVEREIGN_ENGINE_MANIFEST:-}" ]] &&
         sovereign_engine_provision_managed_colima "$SOVEREIGN_ENGINE_MANIFEST"; then
      :
    else
      sovereign_engine_error "no compatible container engine is configured and automatic provisioning could not complete"
      return 1
    fi
  fi
  sovereign_engine_start || {
    sovereign_engine_error "the configured $SOVEREIGN_ENGINE_PROVIDER engine could not be started"
    return 1
  }
  sovereign_engine_probe_basic || {
    sovereign_engine_error "the configured $SOVEREIGN_ENGINE_PROVIDER engine failed Docker or Compose checks"
    return 1
  }
  SOVEREIGN_ENGINE_READY=1
}
