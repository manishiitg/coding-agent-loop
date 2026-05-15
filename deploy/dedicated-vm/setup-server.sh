#!/usr/bin/env bash
# One-time setup for a fresh Ubuntu 24.04 dedicated VM.
# Includes Docker installation, security hardening, and directory setup.
# Run as root on the server.
set -euo pipefail

echo "==> Setting up dedicated VM for MCP Agent Builder..."

# ===================================================================
# 1. System updates
# ===================================================================
echo "==> Updating system packages..."
apt-get update && apt-get upgrade -y

# ===================================================================
# 2. Security hardening
# ===================================================================
echo "==> Hardening server security..."

# Install essential security tools
apt-get install -y ufw fail2ban unattended-upgrades

# --- Firewall (UFW) ---
echo "    Configuring firewall..."
ufw default deny incoming
ufw default allow outgoing
ufw allow 22/tcp    # SSH
ufw allow 80/tcp    # HTTP
ufw allow 443/tcp   # HTTPS
ufw --force enable
echo "    Firewall enabled (SSH, HTTP, HTTPS only)."

# --- Fail2ban (brute-force protection) ---
echo "    Configuring fail2ban..."
cat > /etc/fail2ban/jail.local << 'JAIL'
[DEFAULT]
bantime  = 600
findtime = 600
maxretry = 10

[sshd]
enabled = true
port    = 22
filter  = sshd
logpath = /var/log/auth.log
maxretry = 10
bantime  = 600
JAIL
systemctl enable fail2ban
systemctl restart fail2ban
echo "    Fail2ban enabled (SSH: 10 attempts, 10min ban — safe for dynamic IPs)."

# --- SSH hardening ---
echo "    Hardening SSH..."
# NOTE: We keep PermitRootLogin yes for now (password-based access).
# After setting up SSH keys, run: ./harden-ssh.sh to disable password login.
# Disable empty passwords
sed -i 's/^#\?PermitEmptyPasswords.*/PermitEmptyPasswords no/' /etc/ssh/sshd_config
# Limit auth attempts
sed -i 's/^#\?MaxAuthTries.*/MaxAuthTries 5/' /etc/ssh/sshd_config
# Disable X11 forwarding
sed -i 's/^#\?X11Forwarding.*/X11Forwarding no/' /etc/ssh/sshd_config
systemctl reload ssh
echo "    SSH hardened (password login still enabled - run harden-ssh.sh after setting up keys)."

# --- Automatic security updates ---
echo "    Enabling automatic security updates..."
cat > /etc/apt/apt.conf.d/20auto-upgrades << 'AUTOUPD'
APT::Periodic::Update-Package-Lists "1";
APT::Periodic::Unattended-Upgrade "1";
APT::Periodic::AutocleanInterval "7";
AUTOUPD
echo "    Automatic security updates enabled."

# --- Kernel hardening (sysctl) ---
echo "    Applying kernel hardening..."
cat > /etc/sysctl.d/99-hardening.conf << 'SYSCTL'
# Prevent IP spoofing
net.ipv4.conf.all.rp_filter = 1
net.ipv4.conf.default.rp_filter = 1
# Ignore ICMP redirects
net.ipv4.conf.all.accept_redirects = 0
net.ipv4.conf.default.accept_redirects = 0
net.ipv6.conf.all.accept_redirects = 0
# Don't send ICMP redirects
net.ipv4.conf.all.send_redirects = 0
net.ipv4.conf.default.send_redirects = 0
# Ignore ping broadcasts
net.ipv4.icmp_echo_ignore_broadcasts = 1
# Log suspicious packets
net.ipv4.conf.all.log_martians = 1
net.ipv4.conf.default.log_martians = 1
# Disable IP source routing
net.ipv4.conf.all.accept_source_route = 0
net.ipv6.conf.all.accept_source_route = 0
# SYN flood protection
net.ipv4.tcp_syncookies = 1
net.ipv4.tcp_max_syn_backlog = 2048
net.ipv4.tcp_synack_retries = 2
SYSCTL
sysctl -p /etc/sysctl.d/99-hardening.conf
echo "    Kernel hardened."

# ===================================================================
# 3. Install Docker
# ===================================================================
if ! command -v docker &>/dev/null; then
  echo "==> Installing Docker..."
  apt-get install -y ca-certificates curl gnupg
  install -m 0755 -d /etc/apt/keyrings
  curl -fsSL https://download.docker.com/linux/ubuntu/gpg | gpg --dearmor -o /etc/apt/keyrings/docker.gpg
  chmod a+r /etc/apt/keyrings/docker.gpg
  echo "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/docker.gpg] https://download.docker.com/linux/ubuntu $(. /etc/os-release && echo "$VERSION_CODENAME") stable" > /etc/apt/sources.list.d/docker.list
  apt-get update
  apt-get install -y docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin
  systemctl enable docker
  systemctl start docker
  echo "    Docker installed: $(docker --version)"
else
  echo "    Docker already installed: $(docker --version)"
fi

# ===================================================================
# 4. Create data directories
# ===================================================================
echo "==> Creating data directories..."
mkdir -p /data/docs /data/workspace-db /data/agent-db /data/logs
chmod 755 /data /data/docs /data/workspace-db /data/agent-db /data/logs

# ===================================================================
# 5. Create app user
# ===================================================================
if ! id appuser &>/dev/null; then
  echo "==> Creating appuser..."
  groupadd -g 1001 appgroup || true
  useradd -m -u 1001 -g appgroup -G docker appuser || true
  echo "    appuser created."
else
  echo "    appuser already exists."
fi

chown -R 1001:1001 /data

# ===================================================================
# 6. Create app directory
# ===================================================================
APP_DIR="/opt/mcp-agent"
mkdir -p "$APP_DIR"
chown -R 1001:1001 "$APP_DIR"

# ===================================================================
# 7. Node.js + CLI tools the bare-metal agent shells out to
# ===================================================================
# The agent exec()s these binaries, so they must live on the host (not just
# inside Docker). quick-deploy.sh keeps them up to date on every agent deploy;
# this block bootstraps them on a fresh VM.
if ! command -v node &>/dev/null; then
  echo "==> Installing Node.js 24.x..."
  curl -fsSL https://deb.nodesource.com/setup_24.x | bash -
  apt-get install -y --no-install-recommends nodejs
fi
if ! command -v tmux &>/dev/null; then
  echo "==> Installing tmux for Claude Code interactive provider..."
  apt-get update
  apt-get install -y --no-install-recommends tmux
fi
echo "==> Installing CLI tools (agent-browser, claude, gemini)..."
npm install -g agent-browser@latest @anthropic-ai/claude-code@latest @google/gemini-cli@latest

# Google Chrome — required for browser automation via agent-browser/playwright.
# Some tools (and shell-exec calls like `which google-chrome`) look for
# google-chrome specifically; chromium alone is not a drop-in replacement.
if ! command -v google-chrome &>/dev/null; then
  echo "==> Installing Google Chrome..."
  wget -q -O /tmp/google-chrome.deb https://dl.google.com/linux/direct/google-chrome-stable_current_amd64.deb
  apt-get install -y /tmp/google-chrome.deb
  rm -f /tmp/google-chrome.deb
fi

echo ""
echo "==> Server setup complete!"
echo "    Firewall:   UFW active (22, 80, 443)"
echo "    Fail2ban:   Active (SSH brute-force protection)"
echo "    SSH:        Root password login disabled"
echo "    Auto-update: Security updates enabled"
echo "    Docker:     $(docker --version)"
echo "    Data dir:   /data/"
echo "    App dir:    $APP_DIR"
echo ""
echo "NEXT: Set up SSH keys and lock down password login:"
echo "  1. From your machine: ssh-copy-id root@<this-server>"
echo "  2. On the server, run: /opt/mcp-agent/harden-ssh.sh"
