#!/usr/bin/env bash
set -euo pipefail
command -v python3 >/dev/null

app_home=/var/lib/video-studio
app_dir="$app_home/video-studio"
uid=$(id -u video-studio)
script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
cleanup_source=""
for candidate in "$script_dir/../../common/prune-releases.py" "$script_dir/prune-releases.py"; do
  if [[ -f "$candidate" ]]; then cleanup_source="$candidate"; break; fi
done
[[ -n "$cleanup_source" ]] || { echo 'Copy deploy/common/prune-releases.py alongside this migration script first.' >&2; exit 1; }

usermod -s /bin/bash video-studio
install -d -o video-studio -g video-studio -m 0700 "$app_home/.ssh" "$app_home/.config/systemd/user" "$app_dir/releases"
install -d -o video-studio -g video-studio -m 0755 "$app_home/Downloads" "$app_dir/logs" /data/video-studio/docs/Downloads
install -m 0600 -o video-studio -g video-studio /home/ubuntu/.ssh/authorized_keys "$app_home/.ssh/authorized_keys"

release=$(readlink -f /opt/video-studio/current)
release_name=$(basename "$release")
rsync -a "$release/" "$app_dir/releases/$release_name/"
install -m 0644 "$cleanup_source" "$app_dir/releases/$release_name/prune-releases.py"
ln -sfn "$app_dir/releases/$release_name" "$app_dir/current"
cp /opt/video-studio/.env "$app_dir/.env"
chown -R video-studio:video-studio "$app_dir"
chmod 0600 "$app_dir/.env"
install -m 0644 -o video-studio -g video-studio /tmp/video-studio-rootless/video-studio-*.service "$app_home/.config/systemd/user/"

loginctl enable-linger video-studio
systemctl start "user@${uid}.service"
sleep 1
systemctl disable --now video-studio-gateway video-studio-agent video-studio-workspace
sudo -u video-studio XDG_RUNTIME_DIR="/run/user/$uid" DBUS_SESSION_BUS_ADDRESS="unix:path=/run/user/$uid/bus" systemctl --user daemon-reload
sudo -u video-studio XDG_RUNTIME_DIR="/run/user/$uid" DBUS_SESSION_BUS_ADDRESS="unix:path=/run/user/$uid/bus" systemctl --user enable --now video-studio-workspace video-studio-agent video-studio-gateway
sudo -u video-studio python3 "$app_dir/current/prune-releases.py" "$app_dir" --apply \
  --health-url http://127.0.0.1:8000/api/health --health-url http://127.0.0.1:8080/health
