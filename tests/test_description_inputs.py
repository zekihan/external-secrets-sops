# Contract: peter-evans/dockerhub-description@1b9a80c056b620d92cedb9d9b5a223409c68ddfa
# action.yml requires username/password; src/input-helper.ts only falls back to env
# when those inputs resolve empty. These fixtures intentionally have no global env.
import copy
import re
import unittest
from pathlib import Path
import yaml


class DescriptionInputContractTests(unittest.TestCase):
    def setUp(self):
        self.root = Path(__file__).resolve().parents[1]
        self.doc = yaml.safe_load((self.root / '.github/workflows/docker_hub_readme.yaml').read_text())
        self.steps = [s for j in self.doc['jobs'].values() for s in j.get('steps', [])]
        self.description = next(s for s in self.steps if s.get('uses', '').startswith('peter-evans/dockerhub-description@'))
        self.context = {'secrets.DOCKER_USERNAME': 'fixture-owner',
                        'secrets.DOCKER_PASSWORD': 'fixture-token',
                        'github.event.repository.name': 'fixture-repository'}

    def resolve(self, inputs):
        # GitHub resolves absent secret names to an empty string. Use fake values only.
        return {key: re.sub(r'\$\{\{\s*([^}]+?)\s*\}\}',
                            lambda match: self.context.get(match[1].strip(), ''), value)
                for key, value in inputs.items() if isinstance(value, str)}

    def missing_required(self, inputs):
        resolved = self.resolve(inputs)
        return [name for name in ('username', 'password') if not resolved.get(name)]

    def test_required_inputs_resolve_from_previously_working_secret_names(self):
        self.assertEqual('peter-evans/dockerhub-description@1b9a80c056b620d92cedb9d9b5a223409c68ddfa', self.description['uses'])
        self.assertEqual([], self.missing_required(self.description['with']))
        inputs = self.resolve(self.description['with'])
        self.assertEqual('fixture-owner', inputs['username'])
        self.assertEqual('fixture-token', inputs['password'])
        self.assertEqual('fixture-owner/fixture-repository', inputs['repository'])

    def test_removed_fallback_regression_fails_required_input_contract(self):
        inputs = copy.deepcopy(self.description['with'])
        inputs['username'] = '${{ secrets.DOCKERHUB_USERNAME }}'
        inputs['password'] = '${{ secrets.DOCKERHUB_PASSWORD }}'
        self.assertEqual(['username', 'password'], self.missing_required(inputs))

    def test_absent_or_empty_required_inputs_fail_contract(self):
        for key in ('username', 'password'):
            for value in (None, '', '${{ secrets.UNSET }}'):
                with self.subTest(key=key, value=value):
                    inputs = copy.deepcopy(self.description['with'])
                    if value is None:
                        inputs.pop(key)
                    else:
                        inputs[key] = value
                    self.assertEqual([key], self.missing_required(inputs))

    def test_existing_credentials_are_only_passed_to_description_step(self):
        self.assertNotIn('env', self.doc)
        for job in self.doc['jobs'].values():
            self.assertNotIn('secrets.', str(job.get('env', {})))
        for step in self.steps:
            if step is not self.description:
                self.assertNotIn('secrets.', str(step))
        self.assertNotIn('env', self.description)
