variable "resource_group_name" {
  type        = string
  description = "Name of the existing Azure resource group to use (e.g. code-analysis-phase-1)"
}

variable "location" {
  type        = string
  default     = "eastus"
  description = "Azure region for all resources"
}

variable "project_name" {
  type        = string
  description = "Short project name used for resource naming (e.g. mcpagent)"
}

variable "acr_name" {
  type        = string
  description = "Azure Container Registry name (must be globally unique, alphanumeric only)"
}

# Use ACR admin credentials for image pull when you don't have permission to create role assignments.
# Get after first apply: az acr credential show -n <acr_name> -o tsv --query "passwords[0].value"
variable "acr_admin_password" {
  type        = string
  default     = ""
  sensitive   = true
  description = "ACR admin password for image pull (use when skip_acr_managed_identity=true)"
}

variable "postgres_admin_password" {
  type        = string
  default     = ""
  sensitive   = true
  description = "Admin password for the Azure PostgreSQL Flexible Server"
}

variable "skip_acr_managed_identity" {
  type        = bool
  default     = false
  description = "If true, use ACR admin credentials instead of managed identity (set when you lack role assignment permission)"
}

# Image tags (default to latest; override for releases)
variable "agent_image_tag" {
  type        = string
  default     = "latest"
  description = "Tag for mcp-agent image in ACR"
}

variable "workspace_api_image_tag" {
  type        = string
  default     = "latest"
  description = "Tag for workspace-api image in ACR"
}

variable "frontend_image_tag" {
  type        = string
  default     = "latest"
  description = "Tag for frontend image in ACR (nginx serving built app)"
}

# Optional: URLs for frontend build (if frontend was built with placeholders, set these for runtime env; for build-time see docs)
variable "agent_fqdn" {
  type        = string
  default     = ""
  description = "Public FQDN of the agent Container App (leave empty to use default *.azurecontainerapps.io)"
}

variable "workspace_api_fqdn" {
  type        = string
  default     = ""
  description = "Public FQDN of the workspace-api Container App (leave empty to use default)"
}

# Secrets / env (pass via TF_VAR or tfvars; do not commit real values)
variable "openai_api_key" {
  type        = string
  default     = ""
  sensitive   = true
  description = "OpenAI API key (for agent and optionally workspace-api)"
}

variable "anthropic_api_key" {
  type        = string
  default     = ""
  sensitive   = true
  description = "Anthropic API key (for agent LLM configuration defaults)"
}

variable "agent_env" {
  type        = map(string)
  default     = {}
  sensitive   = true
  description = "Additional env vars for agent (e.g. AGENT_PROVIDER, OPENROUTER_API_KEY)"
}

variable "workspace_api_env" {
  type        = map(string)
  default     = {}
  sensitive   = true
  description = "Additional env vars for workspace-api"
}
