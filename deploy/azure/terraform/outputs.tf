output "public_ip_address" {
  value = azurerm_public_ip.pip.ip_address
}

output "public_fqdn" {
  value = azurerm_public_ip.pip.fqdn
}

output "admin_username" {
  value = var.admin_username
}
