"""Validate the provider's file mounts without a Docker engine."""
from pathlib import Path
import unittest
import yaml

ROOT = Path(__file__).resolve().parents[1]


class ComposeTests(unittest.TestCase):
    def test_provider_requires_existing_bind_sources(self):
        compose = yaml.safe_load((ROOT / 'docker-compose.yaml').read_text())
        volumes = compose['services']['external-secrets-sops']['volumes']
        mounts = {mount['target']: mount for mount in volumes if isinstance(mount, dict)}
        for target in ('/config/config.yaml', '/credentials/age.txt',
                       '/credentials/tls.crt', '/credentials/tls.key'):
            with self.subTest(target=target):
                self.assertIn(target, mounts, 'short bind syntax creates missing host directories')
                mount = mounts[target]
                self.assertEqual(mount['type'], 'bind')
                self.assertIs(mount['read_only'], True)
                self.assertIs(mount['bind']['create_host_path'], False)


if __name__ == '__main__':
    unittest.main()
