from pathlib import Path
import tempfile
import unittest

from deployment_checks import PUBLIC_URL, check_environment_file, check_process_environment


class DeploymentChecksTest(unittest.TestCase):
    def check_file(self, content):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / ".env"
            path.write_text(content)
            check_environment_file(path)

    def test_accepts_exact_public_url_and_systemd_quotes(self):
        for value in [PUBLIC_URL, f'"{PUBLIC_URL}"', f"'{PUBLIC_URL}'"]:
            with self.subTest(value=value):
                self.check_file(f"UNRELATED_SECRET=do-not-print\nPUBLIC_URL={value}\n")

    def test_missing_empty_wrong_host_http_and_duplicate_fail(self):
        cases = [
            "UNRELATED_SECRET=do-not-print\n",
            "PUBLIC_URL=\n",
            "PUBLIC_URL=https://wrong-host.example\n",
            "PUBLIC_URL=http://confida.agentworkshq.com\n",
            f"PUBLIC_URL={PUBLIC_URL}\nPUBLIC_URL={PUBLIC_URL}\n",
            f"PUBLIC_URL={PUBLIC_URL}\nPUBLIC_URL=https://wrong-host.example\n",
            f"PUBLIC_URL={PUBLIC_URL}/api/oauth/callback\n",
        ]
        for content in cases:
            with self.subTest(content=content):
                with self.assertRaises(ValueError) as error:
                    self.check_file(content)
                self.assertNotIn("do-not-print", str(error.exception))

    def test_missing_environment_file_fails(self):
        with tempfile.TemporaryDirectory() as directory:
            with self.assertRaises(FileNotFoundError):
                check_environment_file(Path(directory) / "missing.env")

    def test_checks_actual_process_environment_not_only_saved_config(self):
        check_process_environment(f"SECRET=do-not-print\0PUBLIC_URL={PUBLIC_URL}\0".encode())
        for raw in [b"SECRET=do-not-print\0", b"PUBLIC_URL=\0", b"PUBLIC_URL=https://wrong-host.example\0"]:
            with self.subTest(raw=raw):
                with self.assertRaises(ValueError) as error:
                    check_process_environment(raw)
                self.assertNotIn("do-not-print", str(error.exception))


if __name__ == "__main__":
    unittest.main()
