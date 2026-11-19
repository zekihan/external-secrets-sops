"""Verify the Go test wrapper isolates tests from ambient key sources."""
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[1]


class GoTestWrapperTests(unittest.TestCase):
    def test_test_process_uses_fixture_key_sources(self):
        with tempfile.TemporaryDirectory(prefix='ess-test-wrapper-') as directory:
            fixture = Path(directory)
            ambient = fixture / 'ambient home'
            ambient.mkdir()
            go = fixture / 'go'
            go.write_text(
                '#!/usr/bin/env python3\n'
                'import os, shlex, sys\n'
                'assert sys.argv[1] == "test"\n'
                'runner = shlex.split(sys.argv[3]) if sys.argv[2:3] == ["-exec"] else []\n'
                'command = runner + ["env"]\n'
                'os.execvp(command[0], command)\n'
            )
            go.chmod(0o700)
            home_vars = ('HOME', 'XDG_CONFIG_HOME', 'USERPROFILE', 'APPDATA', 'GNUPGHOME')
            age_vars = ('SOPS_AGE_KEY', 'SOPS_AGE_KEY_CMD', 'SOPS_AGE_SSH_PRIVATE_KEY_FILE',
                        'SOPS_AGE_SSH_PRIVATE_KEY_CMD')
            result = subprocess.run(
                [str(ROOT / 'scripts/go_test.sh')],
                env={
                    'PATH': os.pathsep.join((directory, str(Path(sys.executable).parent), os.defpath)),
                    'TMPDIR': directory,
                    **dict.fromkeys(home_vars, str(ambient)),
                    **dict.fromkeys(age_vars, 'synthetic-ambient-value'),
                },
                check=True, capture_output=True, text=True, timeout=10,
            )
            test_env = dict(line.split('=', 1) for line in result.stdout.splitlines())
            self.assertEqual(test_env['SOPS_AGE_KEY_FILE'], str(ROOT / 'testdata/key.txt'))
            for key in age_vars:
                with self.subTest(key=key):
                    self.assertNotIn(key, test_env)
            for key in home_vars:
                with self.subTest(key=key):
                    self.assertNotEqual(test_env[key], str(ambient))
                    self.assertEqual(Path(test_env[key]).parent, fixture)
            self.assertEqual(set(fixture.iterdir()), {ambient, go})


if __name__ == '__main__':
    unittest.main()
