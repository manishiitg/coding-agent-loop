locals {
  acr_login_server  = azurerm_container_registry.acr.login_server
  acr_username      = azurerm_container_registry.acr.admin_username
  use_acr_admin     = var.skip_acr_managed_identity
  # Use variable if set; otherwise use stable ingress FQDN (does not change when new revisions roll out)
  workspace_api_url = var.workspace_api_fqdn != "" ? var.workspace_api_fqdn : "https://${azurerm_container_app.workspace_api.ingress[0].fqdn}"
}

# ---------------------------------------------------------------------------
# MCP Agent (port 8000)
# ---------------------------------------------------------------------------
resource "azurerm_container_app" "agent" {
  name                         = "${var.project_name}-agent"
  container_app_environment_id = azurerm_container_app_environment.env.id
  resource_group_name         = data.azurerm_resource_group.rg.name
  revision_mode               = "Single"

  dynamic "identity" {
    for_each = local.use_acr_admin ? [] : [1]
    content {
      type         = "UserAssigned"
      identity_ids = [azurerm_user_assigned_identity.acr_pull[0].id]
    }
  }

  dynamic "secret" {
    for_each = toset(local.use_acr_admin && var.acr_admin_password != "" ? [1] : [])
    content {
      name  = "acr-password"
      value = var.acr_admin_password
    }
  }

  dynamic "secret" {
    for_each = toset(var.openai_api_key != "" ? [1] : [])
    content {
      name  = "openai-api-key"
      value = var.openai_api_key
    }
  }

  dynamic "secret" {
    for_each = toset(var.anthropic_api_key != "" ? [1] : [])
    content {
      name  = "anthropic-api-key"
      value = var.anthropic_api_key
    }
  }

  dynamic "secret" {
    for_each = nonsensitive(var.agent_env)
    content {
      name  = "agent-env-${lower(replace(secret.key, "/[._]/", "-"))}"
      value = secret.value
    }
  }

  secret {
    name  = "database-url"
    value = "postgres://pgadmin:${var.postgres_admin_password}@${azurerm_postgresql_flexible_server.db.fqdn}:5432/mcpagent?sslmode=require"
  }

  registry {
    server               = local.acr_login_server
    username             = (local.use_acr_admin && var.acr_admin_password != "") ? local.acr_username : null
    password_secret_name = (local.use_acr_admin && var.acr_admin_password != "") ? "acr-password" : null
    identity             = local.use_acr_admin ? null : azurerm_user_assigned_identity.acr_pull[0].id
  }

  template {
    min_replicas = 1
    max_replicas = 1

    volume {
      name         = "agent-data"
      storage_name = azurerm_container_app_environment_storage.agent_data.name
      storage_type = "AzureFile"
    }

    container {
      name    = "agent"
      image   = "${local.acr_login_server}/mcp-agent:${var.agent_image_tag}"
      cpu     = 1.0
      memory  = "2Gi"
      command = ["./mcp-agent", "server"]
      # MCP config baked into image from deploy/azure/mcp_config.json (same idea as K8s ConfigMap from deploy/k8s/agent/mcp_config.json)
      args    = ["--host", "0.0.0.0", "--port", "8000", "--mcp-config", "/app/configs/mcp_servers_clean_user.json", "--db-type", "postgres"]

      env {
        name  = "PORT"
        value = "8000"
      }
      env {
        name  = "WORKSPACE_API_URL"
        value = local.workspace_api_url
      }
      env {
        name  = "DB_TYPE"
        value = "postgres"
      }
      env {
        name        = "DATABASE_URL"
        secret_name = "database-url"
      }
      env {
        name  = "PUBLIC_URL"
        value = "https://${var.project_name}-agent.${azurerm_container_app_environment.env.default_domain}"
      }
      env {
        name  = "SUPPORTED_LLM_PROVIDERS"
        value = "azure,anthropic"
      }
      # Single-user: no login (default user). Multi-user: set multi_user_mode = true and configure AUTH_* (see docs/multi_user_authentication.md).
      env {
        name  = "MULTI_USER_MODE"
        value = tostring(var.multi_user_mode)
      }
      env {
        name  = "DEFAULT_USER_ID"
        value = "default-user"
      }
      # Hack: The 'value' above is a template. We need to construct the full string inside the container 
      # or use a secret for the whole URL. 
      # Better approach: Pass components or use a secret for the whole URL? 
      # Terraform container apps 'env' with 'secret_name' puts the secret value into the env var. 
      # So we can't interpolate the secret into the value string here directly.
      # Strategy: Pass DB_HOST, DB_USER, DB_PASS separately and let the app handle it OR construct it.
      # The Go agent expects DATABASE_URL.
      # Let's change strategy: use a secure string construction if possible, or pass raw params.
      # Checking Go agent code isn't possible, but standard is DATABASE_URL.
      # I will change to pass DB_PASSWORD as a separate env var and assume the agent can use it, 
      # OR (safer for generic apps) I will create the full connection string as a secret in Terraform.

      dynamic "env" {
        for_each = toset(var.openai_api_key != "" ? [1] : [])
        content {
          name          = "OPENAI_API_KEY"
          secret_name   = "openai-api-key"
        }
      }
      dynamic "env" {
        for_each = toset(var.anthropic_api_key != "" ? [1] : [])
        content {
          name          = "ANTHROPIC_API_KEY"
          secret_name   = "anthropic-api-key"
        }
      }
      dynamic "env" {
        for_each = nonsensitive(var.agent_env)
        content {
          name          = env.key
          secret_name   = "agent-env-${lower(replace(env.key, "/[._]/", "-"))}"
        }
      }

      volume_mounts {
        name = "agent-data"
        path = "/home/appuser/.config/mcpagent"
      }

      readiness_probe {
        transport = "HTTP"
        path      = "/api/health"
        port      = 8000
        interval_seconds = 10
        timeout         = 3
        success_count_threshold = 1
        failure_count_threshold = 3
      }
    }
  }

  ingress {
    external_enabled = true
    target_port      = 8000
    transport        = "http"
    allow_insecure_connections = true

    traffic_weight {
      percentage      = 100
      latest_revision = true
    }
  }
}

# ---------------------------------------------------------------------------
# Workspace API (port 8080)
# ---------------------------------------------------------------------------
resource "azurerm_container_app" "workspace_api" {
  name                         = "${var.project_name}-workspace-api"
  container_app_environment_id = azurerm_container_app_environment.env.id
  resource_group_name         = data.azurerm_resource_group.rg.name
  revision_mode               = "Single"

  dynamic "identity" {
    for_each = local.use_acr_admin ? [] : [1]
    content {
      type         = "UserAssigned"
      identity_ids = [azurerm_user_assigned_identity.acr_pull[0].id]
    }
  }

  dynamic "secret" {
    for_each = toset(local.use_acr_admin && var.acr_admin_password != "" ? [1] : [])
    content {
      name  = "acr-password"
      value = var.acr_admin_password
    }
  }

  dynamic "secret" {
    for_each = toset(var.openai_api_key != "" ? [1] : [])
    content {
      name  = "openai-api-key"
      value = var.openai_api_key
    }
  }

  dynamic "secret" {
    for_each = nonsensitive(var.workspace_api_env)
    content {
      name  = "workspace-env-${lower(replace(secret.key, "/[._]/", "-"))}"
      value = secret.value
    }
  }

  registry {
    server               = local.acr_login_server
    username             = (local.use_acr_admin && var.acr_admin_password != "") ? local.acr_username : null
    password_secret_name = (local.use_acr_admin && var.acr_admin_password != "") ? "acr-password" : null
    identity             = local.use_acr_admin ? null : azurerm_user_assigned_identity.acr_pull[0].id
  }

  template {
    min_replicas = 0
    max_replicas = 3

    volume {
      name         = "workspace-docs"
      storage_name = azurerm_container_app_environment_storage.workspace_docs.name
      storage_type = "AzureFile"
    }

    volume {
      name         = "workspace-data"
      storage_name = azurerm_container_app_environment_storage.workspace_data.name
      storage_type = "AzureFile"
    }

    container {
      name   = "workspace-api"
      image  = "${local.acr_login_server}/workspace-api:${var.workspace_api_image_tag}"
      cpu    = 1.0
      memory = "2Gi"

      env {
        name  = "PORT"
        value = "8080"
      }
      env {
        name  = "DOCS_DIR"
        value = "/app/workspace-docs"
      }
      dynamic "env" {
        for_each = toset(var.openai_api_key != "" ? [1] : [])
        content {
          name          = "OPENAI_API_KEY"
          secret_name   = "openai-api-key"
        }
      }
      dynamic "env" {
        for_each = nonsensitive(var.workspace_api_env)
        content {
          name          = env.key
          secret_name   = "workspace-env-${lower(replace(env.key, "/[._]/", "-"))}"
        }
      }

      volume_mounts {
        name = "workspace-docs"
        path = "/app/workspace-docs"
      }

      volume_mounts {
        name = "workspace-data"
        path = "/app/data"
      }

      startup_probe {
        transport = "HTTP"
        path      = "/health"
        port      = 8080
        interval_seconds        = 10
        timeout                 = 3
        failure_count_threshold = 10
      }
      readiness_probe {
        transport = "HTTP"
        path      = "/health"
        port      = 8080
        interval_seconds = 10
        timeout         = 3
        success_count_threshold = 1
        failure_count_threshold = 3
      }
    }
  }

  ingress {
    external_enabled = true
    target_port      = 8080
    transport        = "http"
    allow_insecure_connections = true

    traffic_weight {
      percentage      = 100
      latest_revision = true
    }
  }
}

# ---------------------------------------------------------------------------
# Frontend (port 80) - nginx serving built app from ACR
# ---------------------------------------------------------------------------
resource "azurerm_container_app" "frontend" {
  name                         = "${var.project_name}-frontend"
  container_app_environment_id = azurerm_container_app_environment.env.id
  resource_group_name         = data.azurerm_resource_group.rg.name
  revision_mode               = "Single"

  dynamic "identity" {
    for_each = local.use_acr_admin ? [] : [1]
    content {
      type         = "UserAssigned"
      identity_ids = [azurerm_user_assigned_identity.acr_pull[0].id]
    }
  }

  dynamic "secret" {
    for_each = toset(local.use_acr_admin && var.acr_admin_password != "" ? [1] : [])
    content {
      name  = "acr-password"
      value = var.acr_admin_password
    }
  }

  registry {
    server               = local.acr_login_server
    username             = (local.use_acr_admin && var.acr_admin_password != "") ? local.acr_username : null
    password_secret_name = (local.use_acr_admin && var.acr_admin_password != "") ? "acr-password" : null
    identity             = local.use_acr_admin ? null : azurerm_user_assigned_identity.acr_pull[0].id
  }

  template {
    min_replicas = 0
    max_replicas = 3

    container {
      name   = "frontend"
      image  = "${local.acr_login_server}/mcp-agent-frontend:${var.frontend_image_tag}"
      cpu    = 0.25
      memory = "0.5Gi"

      readiness_probe {
        transport = "HTTP"
        path      = "/health"
        port      = 80
        interval_seconds = 10
        timeout         = 3
        success_count_threshold = 1
        failure_count_threshold = 3
      }
    }
  }

  ingress {
    external_enabled = true
    target_port      = 80
    transport        = "http"
    allow_insecure_connections = true

    traffic_weight {
      percentage      = 100
      latest_revision = true
    }
  }
}
