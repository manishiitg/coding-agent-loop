variable "resource_group_name" {
  type        = string
  description = "Name of the existing Azure resource group"
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

variable "vm_size" {
  type        = string
  default     = "Standard_B2ms"
  description = "Azure VM size (2 vCPU / 8 GB RAM recommended minimum)"
}

variable "admin_username" {
  type        = string
  default     = "azureuser"
  description = "SSH admin username for the VM"
}

variable "ssh_public_key_path" {
  type        = string
  default     = "~/.ssh/id_rsa.pub"
  description = "Path to SSH public key for VM access"
}

variable "dns_label" {
  type        = string
  description = "DNS label for the VM public IP (results in <label>.<region>.cloudapp.azure.com)"
}

variable "data_disk_size_gb" {
  type        = number
  default     = 32
  description = "Size of the managed data disk in GB"
}

# Secrets / env (pass via TF_VAR or tfvars; do not commit real values)
variable "openai_api_key" {
  type        = string
  default     = ""
  sensitive   = true
  description = "OpenAI API key (for agent)"
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

variable "expose_backend_ports" {
  type        = bool
  default     = false
  description = "If true, expose ports 8000 and 8080 in NSG for debugging"
}
