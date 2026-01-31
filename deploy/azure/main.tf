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

# User-assigned managed identity for pulling images from ACR (skip when using admin credentials)
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
    command = <<-EOT
      ACCOUNT_KEY=$(az storage account keys list \
        --account-name ${azurerm_storage_account.persistent.name} \
        --resource-group ${data.azurerm_resource_group.rg.name} \
        --query '[0].value' -o tsv) && \
      az storage directory create --account-name ${azurerm_storage_account.persistent.name} \
        --account-key "$ACCOUNT_KEY" --share-name ${azurerm_storage_share.workspace_docs.name} \
        --name "Downloads" -o none && \
      az storage directory create --account-name ${azurerm_storage_account.persistent.name} \
        --account-key "$ACCOUNT_KEY" --share-name ${azurerm_storage_share.workspace_docs.name} \
        --name "Chats" -o none && \
      az storage directory create --account-name ${azurerm_storage_account.persistent.name} \
        --account-key "$ACCOUNT_KEY" --share-name ${azurerm_storage_share.workspace_docs.name} \
        --name "Workspace" -o none
    EOT
  }
}
