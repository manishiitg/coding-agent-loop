from pathlib import Path
import unittest


CONFIDA_DIR = Path(__file__).resolve().parent


class ConfidaRuntimeDependenciesTest(unittest.TestCase):
    def test_agent_browser_uses_writable_persistent_prefix_and_is_mandatory(self) -> None:
        deploy = (CONFIDA_DIR / "deploy-rootless-confida.sh").read_text()

        self.assertIn('REMOTE_TOOLS="$REMOTE_APP/tools"', deploy)
        self.assertIn("npm install -g --prefix '$REMOTE_TOOLS' agent-browser@latest", deploy)
        self.assertNotIn("WARNING: agent-browser is NOT installed", deploy)
        self.assertIn("command -v agent-browser >/dev/null", deploy)

    def test_agent_and_workspace_services_receive_tools_path(self) -> None:
        activate = (CONFIDA_DIR / "server-build-and-activate.sh").read_text()

        self.assertIn('runtime_path="$REMOTE_APP/tools/bin:', activate)
        self.assertIn("confida-agent.service.d/zz-runtime-tools.conf", activate)
        self.assertIn("confida-workspace.service.d/zz-runtime-tools.conf", activate)


if __name__ == "__main__":
    unittest.main()
