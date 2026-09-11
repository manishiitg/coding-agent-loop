from pathlib import Path
import unittest


CF_DIR = Path(__file__).resolve().parent


class ConfidaRuntimeDependenciesTest(unittest.TestCase):
    def test_confida_owns_a_pinned_node_24_runtime(self) -> None:
        deploy = (CF_DIR / "deploy-cf.sh").read_text()
        activate = (CF_DIR / "server-build-and-activate.sh").read_text()

        self.assertIn('REMOTE_NODE_VERSION="24.', deploy)
        self.assertIn('REMOTE_NODE_SHA256="', deploy)
        self.assertIn("https://nodejs.org/dist/v$REMOTE_NODE_VERSION/", deploy)
        self.assertIn("sha256sum -c -", deploy)
        self.assertIn("$REMOTE_TOOLS/node/bin:$REMOTE_TOOLS/bin", deploy)
        self.assertIn("/srv/confida/tools/node/bin:/srv/confida/tools/bin", activate)
        self.assertIn('[[ "$(node --version)" == v24.* ]]', activate)

    def test_agent_browser_uses_writable_persistent_prefix_and_is_mandatory(self) -> None:
        deploy = (CF_DIR / "deploy-cf.sh").read_text()

        self.assertIn('REMOTE_TOOLS="$REMOTE_APP/tools"', deploy)
        self.assertIn("npm install -g --prefix '$REMOTE_TOOLS' --allow-scripts=agent-browser agent-browser@latest", deploy)
        self.assertNotIn("WARNING: agent-browser is NOT installed", deploy)
        self.assertIn("command -v agent-browser >/dev/null", deploy)

    def test_agent_and_workspace_services_receive_tools_path(self) -> None:
        activate = (CF_DIR / "server-build-and-activate.sh").read_text()

        self.assertIn('runtime_path="$REMOTE_APP/tools/node/bin:$REMOTE_APP/tools/bin:', activate)
        self.assertIn('awk -v managed_path="$runtime_path"', activate)
        self.assertIn("/^PATH=/", activate)
        self.assertIn("confida-agent.service.d/zz-runtime-tools.conf", activate)
        self.assertIn("confida-workspace.service.d/zz-runtime-tools.conf", activate)
        self.assertIn("/proc/$pid/environ", activate)

    def test_browser_runtime_is_namespaced_to_confida(self) -> None:
        activate = (CF_DIR / "server-build-and-activate.sh").read_text()
        deploy = (CF_DIR / "deploy-cf.sh").read_text()

        for script in (activate, deploy):
            self.assertIn("AGENTWORKS_BROWSER_SESSION_PREFIX=confida", script)
            self.assertIn("AGENTWORKS_BROWSER_STAGING_NAMESPACE=confida", script)
        self.assertIn("confida-agent.service.d/40-browser-isolation.conf", activate)
        self.assertIn("confida-workspace.service.d/40-browser-isolation.conf", activate)


if __name__ == "__main__":
    unittest.main()
