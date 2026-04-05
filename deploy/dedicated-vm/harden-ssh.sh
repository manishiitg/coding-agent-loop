#!/usr/bin/env bash
# Run this AFTER setting up SSH key auth to disable password login.
# Usage: ./harden-ssh.sh
set -euo pipefail

# Verify SSH key is set up before locking out password auth
if [ ! -f /root/.ssh/authorized_keys ] || [ ! -s /root/.ssh/authorized_keys ]; then
  echo "ERROR: No SSH authorized_keys found for root."
  echo "First run: ssh-copy-id root@<this-server> from your local machine."
  exit 1
fi

echo "==> Disabling password authentication..."
sed -i 's/^#\?PermitRootLogin.*/PermitRootLogin prohibit-password/' /etc/ssh/sshd_config
sed -i 's/^#\?PasswordAuthentication.*/PasswordAuthentication no/' /etc/ssh/sshd_config
systemctl reload ssh

echo "==> SSH hardened. Password login is now disabled."
echo "    Only SSH key auth is allowed."
