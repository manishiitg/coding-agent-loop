import importlib.util
from pathlib import Path
import tempfile
import unittest

spec = importlib.util.spec_from_file_location('prune', Path(__file__).with_name('prune-releases.py'))
module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)


class ReleaseCleanupTest(unittest.TestCase):
    def test_only_unused_release_copies_are_removed(self):
        with tempfile.TemporaryDirectory() as tmp:
            app = Path(tmp)
            releases = app / 'releases'
            releases.mkdir()
            names = [f'{i:07x}-20260910120000' for i in range(5)]
            for name in names:
                (releases / name).mkdir()
            (app / 'current').symlink_to(releases / names[0])
            (releases / names[3] / '.deploying').touch()
            (app / 'data').mkdir()
            (releases / 'user-data').mkdir()
            (releases / 'abcdef0-20260910120000').symlink_to(app / 'data')
            proc = app / 'proc'
            (proc / '123').mkdir(parents=True)
            (proc / '123' / 'exe').symlink_to(releases / names[1] / 'bin/agent')
            (proc / '123' / 'cmdline').write_text(f'python\0{releases / names[2]}/bridge.py\0')
            self.assertEqual(module.prune(app, proc=proc), [names[4]])
            self.assertTrue((releases / names[4]).exists())
            self.assertEqual(module.prune(app, apply=True, proc=proc), [names[4]])
            self.assertFalse((releases / names[4]).exists())
            self.assertTrue((app / 'data').is_dir())
            self.assertTrue((releases / 'user-data').is_dir())
            self.assertTrue(all((releases / name).is_dir() for name in names[:4]))

    def test_legacy_named_releases_and_running_scripts(self):
        with tempfile.TemporaryDirectory() as tmp:
            app = Path(tmp).resolve()
            for name in ('current-build', 'old-theme-fix', 'live-browser-panel'):
                (app / 'releases' / name / 'frontend').mkdir(parents=True)
                (app / 'releases' / name / 'frontend/index.html').touch()
            (app / 'current').symlink_to(app / 'releases/current-build')
            proc = app / 'proc'
            (proc / '1').mkdir(parents=True)
            (proc / '1/maps').write_text(f'000 000 000 {app}/releases/live-browser-panel/lib/browser.so')
            self.assertEqual(module.prune(app, apply=True, proc=proc), ['old-theme-fix'])
            self.assertTrue((app / 'releases/live-browser-panel').is_dir())

    def test_invalid_current_aborts_cleanup(self):
        with tempfile.TemporaryDirectory() as tmp:
            app = Path(tmp)
            (app / 'releases').mkdir()
            (app / 'data').mkdir()
            (app / 'current').symlink_to(app / 'data')
            with self.assertRaises(RuntimeError):
                module.prune(app, apply=True)


if __name__ == '__main__':
    unittest.main()
