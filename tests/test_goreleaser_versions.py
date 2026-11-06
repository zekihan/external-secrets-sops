"""Keep PR installation, local release gates and reusable workflows aligned."""
import copy
from pathlib import Path
import re
import unittest
import yaml

ROOT = Path(__file__).resolve().parents[1]
VERSION = re.compile(r'v?([0-9]+\.[0-9]+\.[0-9]+)\Z')


def version_errors(makefile, pr, release):
    assignments = re.findall(r'(?m)^GORELEASER_VERSION\s*:=\s*(\S+)\s*$', makefile)
    if len(assignments) != 1 or not VERSION.fullmatch(assignments[0]):
        return ['Makefile must declare one pinned GORELEASER_VERSION']
    expected = VERSION.fullmatch(assignments[0])[1]
    installers = [step for job in pr.get('jobs', {}).values()
                  for step in job.get('steps', [])
                  if step.get('uses', '').startswith('goreleaser/goreleaser-action@')]
    if not installers:
        return ['PR workflow must install pinned GoReleaser']
    pins = [(f'PR installer {index}', step.get('with', {}).get('version'))
            for index, step in enumerate(installers)]
    pins.extend((f'release job {job}', release.get('jobs', {}).get(job, {}).get('with', {}).get('goreleaser-version'))
                for job in ('publish', 'release'))
    errors = []
    for location, value in pins:
        match = VERSION.fullmatch(value) if isinstance(value, str) else None
        if not match or match[1] != expected:
            errors.append(f'{location}: expected GoReleaser {expected}, got {value!r}')
    return errors


class GoReleaserVersionTests(unittest.TestCase):
    def setUp(self):
        self.makefile = 'GORELEASER_VERSION := 2.20.0\n'
        self.pr = {'jobs': {'pr': {'steps': [{
            'uses': 'goreleaser/goreleaser-action@' + 'a' * 40,
            'with': {'version': 'v2.20.0'},
        }]}}}
        self.release = {'jobs': {name: {'with': {'goreleaser-version': 'v2.20.0'}}
                                 for name in ('publish', 'release')}}

    def test_repository_pins_match(self):
        pr = yaml.safe_load((ROOT / '.github/workflows/pr.yaml').read_text())
        release = yaml.safe_load((ROOT / '.github/workflows/release.yaml').read_text())
        self.assertEqual(version_errors((ROOT / 'Makefile').read_text(), pr, release), [])

    def test_consistent_pins_accept_optional_v_prefix(self):
        self.assertEqual(version_errors(self.makefile, self.pr, self.release), [])
        self.pr['jobs']['pr']['steps'][0]['with']['version'] = '2.20.0'
        self.assertEqual(version_errors(self.makefile, self.pr, self.release), [])

    def test_pr_installer_mismatch_or_missing_pin_fails(self):
        for version in ('v2.19.0', 'latest', '${{ inputs.version }}', None):
            with self.subTest(version=version):
                self.pr['jobs']['pr']['steps'][0]['with']['version'] = version
                self.assertTrue(version_errors(self.makefile, self.pr, self.release))

    def test_each_reusable_release_job_requires_matching_pin(self):
        for job in ('publish', 'release'):
            for version in ('v2.19.0', 'latest', None):
                with self.subTest(job=job, version=version):
                    release = copy.deepcopy(self.release)
                    release['jobs'][job]['with']['goreleaser-version'] = version
                    self.assertTrue(version_errors(self.makefile, self.pr, release))

    def test_missing_installer_or_release_job_fails(self):
        self.assertTrue(version_errors(self.makefile, {'jobs': {}}, self.release))
        self.assertTrue(version_errors(self.makefile, self.pr, {'jobs': {}}))

    def test_makefile_pin_mismatch_missing_or_duplicate_fails(self):
        for makefile in ('GORELEASER_VERSION := 2.19.0\n', '',
                         'GORELEASER_VERSION := latest\n', self.makefile * 2):
            with self.subTest(makefile=makefile):
                self.assertTrue(version_errors(makefile, self.pr, self.release))


if __name__ == '__main__':
    unittest.main()
