output "public_ip_address" {
  value = azurerm_public_ip.pip.ip_address
}

output "public_fqdn" {
  value = azurerm_public_ip.pip.fqdn
}

output "acr_name" {
  value = azurerm_container_registry.acr.name
}

output "acr_login_server" {
  value = azurerm_container_registry.acr.login_server
}

output "admin_username" {
  value = var.admin_username
}
