"""Regression checks for the shared rootless Linux deployment pipeline."""
from pathlib import Path
import unittest


ROOT = Path(__file__).resolve().parent
REPO = ROOT.parents[1]


class SharedRootlessDeploymentTest(unittest.TestCase):
    def test_products_only_define_configuration_and_assets(self):
        for product in ("confida", "sparkquill"):
            directory = ROOT / "products" / product
            with self.subTest(product=product):
                self.assertTrue((directory / "product.env").is_file())
                self.assertTrue((directory / "runtime-config.js").is_file())
                self.assertTrue((directory / "mcp-servers.json").is_file())
                self.assertFalse(any(directory.glob("*.sh")))

    def test_confida_compatibility_entrypoint_has_no_deploy_implementation(self):
        wrapper = (REPO / "deploy/cf/deploy-cf.sh").read_text()
        self.assertIn('rootless-linux/deploy.sh" confida', wrapper)
        for implementation_detail in ("npm install", "go build", "systemctl", "curl -fsSL"):
            self.assertNotIn(implementation_detail, wrapper)

    def test_confida_keeps_required_product_contract(self):
        config = (ROOT / "products/confida/product.env").read_text()
        for expected in (
            "PIN_NODE_VERSION=\"24.21.0\"",
            "CLI_TOOLS=(claude codex pi cursor muse)",
            "COPY_PLAYBOOKS=true",
            "RUN_WORKFLOW_BUILDER_MIGRATION=true",
            "PERSIST_MCP_STATE=true",
            "PUBLIC_HEALTH_STATUS=200",
            'PUBLIC_CHECK_PATHS=("/login")',
            "AGENTWORKS_MCP_STATE_DIR=/srv/confida/state/mcp",
        ):
            self.assertIn(expected, config)

    def test_shared_builder_owns_confida_runtime_guards(self):
        deploy = (ROOT / "deploy.sh").read_text()
        build = (ROOT / "build-and-activate.sh").read_text()
        for expected in ("install-slack-cli.sh", "agent-browser@latest", "CLI_TOOLS", "to_https_url"):
            self.assertIn(expected, deploy)
        for expected in (
            "RUNTIME_CONFIG_REQUIRED_SNIPPETS",
            "PLAYBOOK_SMOKE_PATH",
            "RUN_WORKFLOW_BUILDER_MIGRATION",
            "RUN_PRODUCT_SECRETS_MIGRATION",
            "PERSIST_MCP_STATE",
            "AGENT_EXTRA_ENV",
            "deployment_checks.py",
            "prune-releases.py",
        ):
            self.assertIn(expected, build)


if __name__ == "__main__":
    unittest.main()
