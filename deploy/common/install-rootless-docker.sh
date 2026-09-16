#!/usr/bin/env bash
# Install or verify a private Docker daemon for an unprivileged Linux service.
# Usage: install-rootless-docker.sh [--check] <service-user>
set -euo pipefail

mode=install
if [[ "${1:-}" == "--check" ]]; then
  mode=check
  shift
fi
service_user="${1:?service user is required}"
[[ "$(uname -s)" == Linux ]] || { echo 'Rootless Docker is supported here only on Linux.' >&2; exit 1; }
id -u "$service_user" >/dev/null 2>&1 || { echo "Missing service user: $service_user" >&2; exit 1; }

service_uid="$(id -u "$service_user")"
service_home="$(getent passwd "$service_user" | cut -d: -f6)"
runtime_dir="/run/user/$service_uid"
docker_host="unix://$runtime_dir/docker.sock"
if [[ "$(id -u)" == "$service_uid" ]]; then
  user_systemctl=(env "HOME=$service_home" "XDG_RUNTIME_DIR=$runtime_dir" "DBUS_SESSION_BUS_ADDRESS=unix:path=$runtime_dir/bus" systemctl --user)
  user_docker=(env "HOME=$service_home" "XDG_RUNTIME_DIR=$runtime_dir" "DOCKER_HOST=$docker_host" docker)
elif [[ "$(id -u)" == 0 ]]; then
  user_systemctl=(runuser -u "$service_user" -- env "HOME=$service_home" "XDG_RUNTIME_DIR=$runtime_dir" "DBUS_SESSION_BUS_ADDRESS=unix:path=$runtime_dir/bus" systemctl --user)
  user_docker=(runuser -u "$service_user" -- env "HOME=$service_home" "XDG_RUNTIME_DIR=$runtime_dir" "DOCKER_HOST=$docker_host" docker)
else
  echo "Run this check as root or as $service_user." >&2
  exit 1
fi

allocate_subid() {
  local file="$1" option="$2" start
  grep -q "^${service_user}:" "$file" && return
  start="$(awk -F: 'BEGIN { max=100000; block=65536 } NF == 3 { end=$2+$3; if (end > max) max=end } END { print int((max+block-1)/block)*block }' /etc/subuid /etc/subgid)"
  usermod "$option" "$start-$((start + 65535))" "$service_user"
}

if [[ "$mode" == install ]]; then
  [[ "$(id -u)" == 0 ]] || { echo 'Rootless Docker installation must run as root.' >&2; exit 1; }
  command -v apt-get >/dev/null || { echo 'This installer currently supports Debian/Ubuntu hosts.' >&2; exit 1; }
  export DEBIAN_FRONTEND=noninteractive
  apt-get update -qq
  if ! apt-cache show docker-ce >/dev/null 2>&1; then
    apt-get install -y ca-certificates curl gnupg
    . /etc/os-release
    case "${ID:-}" in debian|ubuntu) ;; *) echo "Unsupported distribution: ${ID:-unknown}" >&2; exit 1 ;; esac
    install -m 0755 -d /etc/apt/keyrings
    curl -fsSL "https://download.docker.com/linux/$ID/gpg" | gpg --batch --yes --dearmor -o /etc/apt/keyrings/docker.gpg
    chmod a+r /etc/apt/keyrings/docker.gpg
    printf 'deb [arch=%s signed-by=/etc/apt/keyrings/docker.gpg] https://download.docker.com/linux/%s %s stable\n' \
      "$(dpkg --print-architecture)" "$ID" "$VERSION_CODENAME" > /etc/apt/sources.list.d/docker.list
    apt-get update -qq
  fi
  apt-get install -y docker-ce docker-ce-cli docker-buildx-plugin docker-compose-plugin docker-ce-rootless-extras uidmap slirp4netns fuse-overlayfs
  allocate_subid /etc/subuid --add-subuids
  allocate_subid /etc/subgid --add-subgids
  loginctl enable-linger "$service_user"
  systemctl start "user@${service_uid}.service"
  "${user_systemctl[@]}" daemon-reload
  runuser -u "$service_user" -- env "HOME=$service_home" "XDG_RUNTIME_DIR=$runtime_dir" "DBUS_SESSION_BUS_ADDRESS=unix:path=$runtime_dir/bus" dockerd-rootless-setuptool.sh install --force
  "${user_systemctl[@]}" enable --now docker.service
fi

command -v docker >/dev/null || { echo 'Docker CLI is unavailable.' >&2; exit 1; }
security_options=""
for _ in $(seq 1 30); do
  if [[ -S "$runtime_dir/docker.sock" ]]; then
    security_options="$("${user_docker[@]}" info --format '{{json .SecurityOptions}}' 2>/dev/null || true)"
    [[ -n "$security_options" ]] && break
  fi
  sleep 1
done
[[ -S "$runtime_dir/docker.sock" ]] || { echo "Rootless Docker socket is unavailable: $runtime_dir/docker.sock" >&2; exit 1; }
[[ -n "$security_options" ]] || { echo 'Rootless Docker daemon did not become ready.' >&2; exit 1; }
[[ "$(stat -c %u "$runtime_dir/docker.sock")" == "$service_uid" ]] || { echo 'Rootless Docker socket has the wrong owner.' >&2; exit 1; }
"${user_systemctl[@]}" is-enabled --quiet docker.service
"${user_systemctl[@]}" is-active --quiet docker.service
grep -Fq 'name=rootless' <<<"$security_options" || { echo 'Docker daemon is not rootless.' >&2; exit 1; }
docker_root="$("${user_docker[@]}" info --format '{{.DockerRootDir}}')"
case "$docker_root" in
  "$service_home"/*) ;;
  *) echo "Docker data root escapes the service home: $docker_root" >&2; exit 1 ;;
esac
if id -nG "$service_user" | tr ' ' '\n' | grep -Fxq docker; then
  echo "$service_user must not belong to the privileged docker group." >&2
  exit 1
fi

printf 'rootless Docker ready: user=%s socket=%s data=%s\n' "$service_user" "$docker_host" "$docker_root"
