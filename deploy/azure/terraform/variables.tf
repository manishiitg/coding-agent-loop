variable "resource_group_name" {
  description = "Name of the resource group"
  type        = string
  default     = "mcpagent-rg"
}

variable "location" {
  description = "Azure region"
  type        = string
  default     = "swedencentral"
}

variable "vm_name" {
  description = "Name of the Virtual Machine"
  type        = string
  default     = "mcpagent-vm-v2"
}

variable "admin_username" {
  description = "Admin username for the VM"
  type        = string
  default     = "appuser"
}

variable "vm_size" {
  description = "Size of the VM"
  type        = string
  default     = "Standard_D2s_v3" # 2 vCPU, 8 GB RAM
}

variable "ssh_public_key" {
  description = "SSH Public Key for access"
  type        = string
}

variable "dns_label" {
  description = "DNS label for the Public IP (must be unique in the region)"
  type        = string
  default     = "mcpagent-vm-v2"
}

variable "acr_name" {
  description = "Name of the Azure Container Registry (must be globally unique)"
  type        = string
  default     = "mcpagentacr"
}
