output "vm_public_ip" {
  value       = azurerm_public_ip.pip.ip_address
  description = "Public IP address of the VM"
}

output "vm_fqdn" {
  value       = azurerm_public_ip.pip.fqdn
  description = "Fully qualified domain name of the VM"
}

output "ssh_command" {
  value       = "ssh ${var.admin_username}@${azurerm_public_ip.pip.fqdn}"
  description = "SSH command to connect to the VM"
}

output "frontend_url" {
  value       = "http://${azurerm_public_ip.pip.fqdn}"
  description = "Frontend URL (served by nginx)"
}

output "agent_url" {
  value       = "http://${azurerm_public_ip.pip.fqdn}/api"
  description = "Agent API URL (proxied through nginx)"
}

output "workspace_api_url" {
  value       = "http://${azurerm_public_ip.pip.fqdn}/workspace-api"
  description = "Workspace API URL (proxied through nginx)"
}
