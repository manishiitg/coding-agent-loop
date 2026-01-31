output "resource_group_name" {
  value       = data.azurerm_resource_group.rg.name
  description = "Resource group name (existing)"
}

output "acr_login_server" {
  value       = azurerm_container_registry.acr.login_server
  description = "ACR login server (use for docker push)"
}

output "acr_name" {
  value       = azurerm_container_registry.acr.name
  description = "ACR name"
}

output "agent_fqdn" {
  value       = "https://${azurerm_container_app.agent.ingress[0].fqdn}"
  description = "Public URL of the MCP Agent (use as VITE_API_BASE_URL when rebuilding frontend)"
}

output "workspace_api_fqdn" {
  value       = "https://${azurerm_container_app.workspace_api.ingress[0].fqdn}"
  description = "Public URL of the Workspace API (use as VITE_WORKSPACE_API_URL when rebuilding frontend)"
}

output "frontend_fqdn" {
  value       = "https://${azurerm_container_app.frontend.ingress[0].fqdn}"
  description = "Public URL of the Frontend (Container App with nginx)"
}
