# Deploy

Deployment configs and scripts for Runloop.

| Target | Path | Description |
|--------|------|-------------|
| **Dedicated VM** (prod) | [deploy/dedicated-vm/](dedicated-vm/) | Hetzner VM, hybrid Docker + bare-metal systemd. Live at https://agents.excellencetechnologies.in |
| **Kubernetes** | [deploy/k8s/](k8s/) | Manifests (agent, frontend, workspace-api), shared config, and deploy script |
| **Azure** | [deploy/azure/](azure/) | Terraform for Azure Container Apps |

- **Dedicated VM**: `cd deploy/dedicated-vm && ./quick-deploy.sh all`. See [dedicated-vm/README.md](dedicated-vm/README.md) for access, architecture, and gotchas.
- **K8s**: run `./deploy/k8s/scripts/deploy-k8s.sh` from repo root. See [k8s/README.md](k8s/README.md).
- **Azure**: `cd deploy/azure` then Terraform / `deploy.sh`. See [azure/README.md](azure/README.md).
