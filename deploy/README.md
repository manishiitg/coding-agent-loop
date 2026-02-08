# Deploy

Deployment configs and scripts for MCP Agent Builder.

| Target | Path | Description |
|--------|------|-------------|
| **Kubernetes** | [deploy/k8s/](k8s/) | Manifests (agent, frontend, workspace-api), shared config, and deploy script |
| **Azure** | [deploy/azure/](azure/) | Terraform for Azure Container Apps |

- **K8s**: run `./deploy/k8s/scripts/deploy-k8s.sh` from repo root. See [k8s/README.md](k8s/README.md).
- **Azure**: `cd deploy/azure` then Terraform / `deploy.sh`. See [azure/README.md](azure/README.md).
