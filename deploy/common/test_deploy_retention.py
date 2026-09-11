"""Keep all directory-based Linux deployment entry points on shared cleanup."""
from pathlib import Path
import subprocess
import unittest

DEPLOY = Path(__file__).resolve().parents[1]


class DeploymentRetentionTest(unittest.TestCase):
    def test_release_installers_require_agent_and_workspace_health(self):
        installers = {
            'aws-ec2/deploy-rootless.sh': (8000, 8080),
            'aws-ec2/server/install-release.sh': (8000, 8080),
            'aws-ec2/rootless/migrate-once.sh': (8000, 8080),
            'cf/server-build-and-activate.sh': (22000, 22001),
            'dedicated-vm/deploy-dominion.sh': (21000, 21001),
        }
        for name, (agent, workspace) in installers.items():
            with self.subTest(script=name):
                script = (DEPLOY / name).read_text()
                self.assertIn('prune-releases.py', script)
                self.assertIn('--apply', script)
                self.assertIn(f'--health-url http://127.0.0.1:{agent}/api/health', script)
                self.assertIn(f'--health-url http://127.0.0.1:{workspace}/health', script)

    def test_every_release_builder_packages_the_shared_helper(self):
        for name in ('aws-ec2/deploy-rootless.sh', 'aws-ec2/deploy-aws-ec2.sh', 'cf/server-build-and-activate.sh', 'dedicated-vm/deploy-dominion.sh'):
            with self.subTest(script=name):
                self.assertIn('/deploy/common/prune-releases.py', (DEPLOY / name).read_text())

    def test_linux_shell_scripts_parse(self):
        for path in DEPLOY.rglob('*.sh'):
            with self.subTest(script=str(path.relative_to(DEPLOY))):
                subprocess.run(['bash', '-n', str(path)], check=True, capture_output=True)


if __name__ == '__main__':
    unittest.main()
