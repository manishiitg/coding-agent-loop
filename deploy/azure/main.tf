# Use existing resource group (e.g. code-analysis-phase-1)
data "azurerm_resource_group" "rg" {
  name = var.resource_group_name
}

# Azure Container Registry for app images
resource "azurerm_container_registry" "acr" {
  name                = var.acr_name
  resource_group_name = data.azurerm_resource_group.rg.name
  location            = data.azurerm_resource_group.rg.location
  sku                 = "Basic"
  admin_enabled       = true
}

# Log Analytics workspace (required for Container App Environment)
resource "azurerm_log_analytics_workspace" "law" {
  name                = "${var.project_name}-law"
  resource_group_name = data.azurerm_resource_group.rg.name
  location            = data.azurerm_resource_group.rg.location
  sku                 = "PerGB2018"
  retention_in_days   = 30
}

# Container App Environment (shared for all three apps)
resource "azurerm_container_app_environment" "env" {
  name                       = "${var.project_name}-env"
  resource_group_name        = data.azurerm_resource_group.rg.name
  location                   = data.azurerm_resource_group.rg.location
  log_analytics_workspace_id = azurerm_log_analytics_workspace.law.id
}

# --------------------------------------------------------------------------- 
# Persistent Storage (Azure Files)
# --------------------------------------------------------------------------- 
resource "azurerm_storage_account" "persistent" {
  name                     = "${replace(var.project_name, "-", "")}storage"
  resource_group_name      = data.azurerm_resource_group.rg.name
  location                 = data.azurerm_resource_group.rg.location
  account_tier             = "Standard"
  account_replication_type = "LRS"
}

resource "azurerm_storage_share" "workspace_docs" {
  name               = "workspace-docs"
  storage_account_name = azurerm_storage_account.persistent.name
  quota              = 5 # GB
}

resource "azurerm_storage_share" "workspace_data" {
  name               = "workspace-data"
  storage_account_name = azurerm_storage_account.persistent.name
  quota              = 5 # GB
}

resource "azurerm_storage_share" "agent_data" {
  name               = "agent-data"
  storage_account_name = azurerm_storage_account.persistent.name
  quota              = 2 # GB - for SQLite DB, OAuth tokens, configs
}

# Mount Azure File Shares in Container App Environment
resource "azurerm_container_app_environment_storage" "workspace_docs" {
  name                         = "workspace-docs"
  container_app_environment_id = azurerm_container_app_environment.env.id
  account_name                 = azurerm_storage_account.persistent.name
  share_name                   = azurerm_storage_share.workspace_docs.name
  access_key                   = azurerm_storage_account.persistent.primary_access_key
  access_mode                  = "ReadWrite"
}

resource "azurerm_container_app_environment_storage" "workspace_data" {
  name                         = "workspace-data"
  container_app_environment_id = azurerm_container_app_environment.env.id
  account_name                 = azurerm_storage_account.persistent.name
  share_name                   = azurerm_storage_share.workspace_data.name
  access_key                   = azurerm_storage_account.persistent.primary_access_key
  access_mode                  = "ReadWrite"
}

resource "azurerm_container_app_environment_storage" "agent_data" {
  name                         = "agent-data"
  container_app_environment_id = azurerm_container_app_environment.env.id
  account_name                 = azurerm_storage_account.persistent.name
  share_name                   = azurerm_storage_share.agent_data.name
  access_key                   = azurerm_storage_account.persistent.primary_access_key
  access_mode                  = "ReadWrite"
}

# User-assigned managed identity for pulling images from ACR (only when not using admin credentials)
resource "azurerm_user_assigned_identity" "acr_pull" {
  count               = var.skip_acr_managed_identity ? 0 : 1
  name                = "${var.project_name}-acr-pull"
  resource_group_name = data.azurerm_resource_group.rg.name
  location            = data.azurerm_resource_group.rg.location
}

resource "azurerm_role_assignment" "acr_pull" {
  count                = var.skip_acr_managed_identity ? 0 : 1
  scope                = azurerm_container_registry.acr.id
  role_definition_name = "AcrPull"
  principal_id         = azurerm_user_assigned_identity.acr_pull[0].principal_id
}

# --------------------------------------------------------------------------- 
# Default Workspace Folders (created once when the file share is provisioned)
# --------------------------------------------------------------------------- 
resource "null_resource" "workspace_default_folders" {
  triggers = {
    share_id = azurerm_storage_share.workspace_docs.id
  }

  provisioner "local-exec" {
    command = <<EOT
      ACCOUNT_KEY=$(az storage account keys list --account-name ${azurerm_storage_account.persistent.name} --resource-group ${data.azurerm_resource_group.rg.name} --query '[0].value' -o tsv) && az storage directory create --account-name ${azurerm_storage_account.persistent.name} --account-key "$ACCOUNT_KEY" --share-name ${azurerm_storage_share.workspace_docs.name} --name "Downloads" -o none && az storage directory create --account-name ${azurerm_storage_account.persistent.name} --account-key "$ACCOUNT_KEY" --share-name ${azurerm_storage_share.workspace_docs.name} --name "Chats" -o none && az storage directory create --account-name ${azurerm_storage_account.persistent.name} --account-key "$ACCOUNT_KEY" --share-name ${azurerm_storage_share.workspace_docs.name} --name "Workspace" -o none
    EOT
  }
}

# ---------------------------------------------------------------------------
# Database (PostgreSQL Flexible Server)
# ---------------------------------------------------------------------------
resource "random_id" "db_suffix" {
  byte_length = 4
}

resource "azurerm_postgresql_flexible_server" "db" {
  name                   = "${var.project_name}-db-${random_id.db_suffix.hex}"
  resource_group_name    = data.azurerm_resource_group.rg.name
  location               = data.azurerm_resource_group.rg.location
  version                = "16"
  administrator_login    = "pgadmin"
  administrator_password = var.postgres_admin_password
  storage_mb             = 32768
  sku_name               = "B_Standard_B1ms" # Burstable, 1 vCore, 2GB RAM
  zone                   = "3"
  
  # Allow public access (required for Container Apps without VNET integration)
  # Security note: Access is restricted by firewall rules below.
  public_network_access_enabled = true
}
resource "azurerm_postgresql_flexible_server_database" "mcpagent" {
  name      = "mcpagent"
  server_id = azurerm_postgresql_flexible_server.db.id
  charset   = "UTF8"
  collation = "en_US.utf8"
}

# Allow access from Azure Services (including Container Apps)
resource "azurerm_postgresql_flexible_server_firewall_rule" "azure_services" {
  name             = "allow-azure-services"
  server_id        = azurerm_postgresql_flexible_server.db.id
  start_ip_address = "0.0.0.0"
  end_ip_address   = "0.0.0.0"
}

# Allow uuid-ossp extension (required for agent migrations)
resource "azurerm_postgresql_flexible_server_configuration" "uuid_ossp" {
  name      = "azure.extensions"
  server_id = azurerm_postgresql_flexible_server.db.id
  value     = "uuid-ossp"
}

# Allow Terraform runner's public IP so local-exec can run CREATE EXTENSION
data "http" "runner_ip" {
  count = var.allow_terraform_runner_ip ? 1 : 0
  url   = "https://ifconfig.me/ip"
}
resource "azurerm_postgresql_flexible_server_firewall_rule" "terraform_runner" {
  count            = var.allow_terraform_runner_ip ? 1 : 0
  name             = "allow-terraform-runner"
  server_id        = azurerm_postgresql_flexible_server.db.id
  start_ip_address = trimspace(var.terraform_runner_public_ip != "" ? var.terraform_runner_public_ip : data.http.runner_ip[0].response_body)
  end_ip_address   = trimspace(var.terraform_runner_public_ip != "" ? var.terraform_runner_public_ip : data.http.runner_ip[0].response_body)
}

# Create uuid-ossp extension in mcpagent (runs on machine executing terraform apply; requires psql)
resource "null_resource" "postgres_uuid_ossp" {
  depends_on = [
    azurerm_postgresql_flexible_server_database.mcpagent,
    azurerm_postgresql_flexible_server_configuration.uuid_ossp,
    azurerm_postgresql_flexible_server_firewall_rule.terraform_runner,
  ]

  triggers = {
    server_id = azurerm_postgresql_flexible_server.db.id
  }

  provisioner "local-exec" {
    command     = "psql \"host=${azurerm_postgresql_flexible_server.db.fqdn} port=5432 dbname=mcpagent user=pgadmin sslmode=require\" -c 'CREATE EXTENSION IF NOT EXISTS \"uuid-ossp\";'"
    environment = {
      PGPASSWORD = var.postgres_admin_password
    }
  }
}