"""Offline regression checks for the former workflow-global secret path."""
from pathlib import Path
import re
import unittest

ROOT = Path(__file__).resolve().parents[1]


def broad_secret_scopes(text):
    """Find secrets in workflow/job env sections before individual steps."""
    bad = []
    for match in re.finditer(r'(?m)^( {0,4})env:\s*\n((?:(?: +[^\n]*|)\n)*)', text):
        indent = len(match[1])
        lines = []
        for line in match[2].splitlines():
            if line.strip() and len(line) - len(line.lstrip()) <= indent:
                break
            lines.append(line)
        if 'secrets.' in '\n'.join(lines):
            bad.append(match.start())
    return bad


class CredentialScopeTests(unittest.TestCase):
    def test_negative_global_and_job_secret_fixtures(self):
        for indent in ('', '    '):
            fixture = indent + 'env:\n' + indent + '  TOKEN: ${{ secrets.DOCKER_PASSWORD }}\n'
            self.assertTrue(broad_secret_scopes(fixture))

    def test_positive_step_scoped_secret_fixture(self):
        fixture = '      - name: Publish\n        env:\n          TOKEN: ${{ secrets.DOCKER_PASSWORD }}\n'
        self.assertEqual(broad_secret_scopes(fixture), [])

    def test_credentials_are_not_inherited_by_checkout(self):
        for path in (ROOT / '.github/workflows').glob('*.yaml'):
            text = path.read_text()
            with self.subTest(workflow=path.name):
                self.assertEqual(broad_secret_scopes(text), [])
                for step in re.split(r'(?m)^      - ', text)[1:]:
                    if 'uses: actions/checkout@' in step:
                        self.assertIn('persist-credentials: false', step)
                        self.assertNotIn('secrets.', step)
        description = (ROOT / '.github/workflows/docker_hub_readme.yaml').read_text()
        self.assertNotIn('login-action@', description)
        self.assertIn('secrets.DOCKERHUB_PASSWORD', description)

    def test_write_permissions_are_job_scoped(self):
        for path in (ROOT / '.github/workflows').glob('*.yaml'):
            text = path.read_text()
            global_permissions = re.search(r'(?m)^permissions:\n((?:  [^\n]+\n)*)', text)
            self.assertIsNotNone(global_permissions, path.name)
            self.assertNotIn(': write', global_permissions[1], path.name)
