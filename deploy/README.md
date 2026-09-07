# Deploy

Deployment configs and scripts for Runloop.

Every frontend release must pass `node frontend/scripts/check-release-assets.mjs <packaged-static-directory>`.
`npm run build` includes this gate. Repeat it against uploaded assets before
activation and configure the agent's `STATIC_DIR` to the checked directory.
See the [Linux release gate](ROOTLESS-LINUX-DEPLOYMENT-CHECKLIST.md#release-gate-report-preview-assets-every-release), including split-container requirements.

| Target | Path | Description |
|--------|------|-------------|
| **Dedicated VM** (legacy, shared host) | [deploy/dedicated-vm/](dedicated-vm/) | Hetzner VM, hybrid Docker + bare-metal systemd, root-owned. Live at https://agents.excellencetechnologies.in |
| **Dominion** (dedicated VM) | [deploy/dedicated-vm/dominion-hetzner.md](dedicated-vm/dominion-hetzner.md) | Isolated Hetzner VM, rootless `systemd --user`. Live at https://trader.tectonicmarkets.com |
| **Video Studio** (AWS EC2) | [deploy/aws-ec2/](aws-ec2/) | Isolated EC2 host, rootless `systemd --user`. Live at https://video.realtrainingsys.com |
| **Kubernetes** | [deploy/k8s/](k8s/) | Manifests (agent, frontend, workspace-api), shared config, and deploy script |
| **Azure** | [deploy/azure/](azure/) | Terraform for Azure Container Apps |

- **Dedicated VM** (legacy): `cd deploy/dedicated-vm && ./quick-deploy.sh all`. See [dedicated-vm/README.md](dedicated-vm/README.md) for access, architecture, and gotchas.
- **Dominion**: see [dedicated-vm/dominion-hetzner.md](dedicated-vm/dominion-hetzner.md) — no automated deploy script; manual `rsync` + `systemctl --user restart`, documented step by step.
- **Video Studio**: `bash deploy/aws-ec2/deploy-rootless.sh`. See [aws-ec2/README.md](aws-ec2/README.md).
- **K8s**: run `./deploy/k8s/scripts/deploy-k8s.sh` from repo root. See [k8s/README.md](k8s/README.md).
- **Azure**: `cd deploy/azure` then Terraform / `deploy.sh`. See [azure/README.md](azure/README.md).

Dominion and Video Studio share the same rootless `systemd --user` +
host-level-Caddy + Landlock-sandboxed architecture. Before standing up a new
deployment on that pattern — or after any change to the shared `workspace`/
`agent_go` sandboxing or Caddy config — run through
[`ROOTLESS-LINUX-DEPLOYMENT-CHECKLIST.md`](ROOTLESS-LINUX-DEPLOYMENT-CHECKLIST.md).
It exists because Dominion, which has no deploy script, independently
rediscovered every item on it as a live production incident.
