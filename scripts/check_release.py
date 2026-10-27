#!/usr/bin/env python3
"""Verify release archives and exercise the native resolver using synthetic inputs."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import platform
import subprocess
import tarfile
import tempfile
import zipfile


def run(binary, *args, input=None):
    env = {k: v for k, v in os.environ.items() if not k.startswith(('SOPS_', 'ESS_', 'AGE_'))}
    env['SOPS_AGE_KEY_FILE'] = '/nonexistent/release-check-no-identities'
    return subprocess.run([str(binary), *args], env=env, capture_output=True, check=False, input=input)


def check_resolver(binary, version, commit):
    result = run(binary, 'version')
    assert result.returncode == 0 and not result.stderr
    assert result.stdout.decode().startswith(f'external-secrets-sops {version} ({commit}, '), 'version/commit mismatch'
    with tempfile.TemporaryDirectory() as directory:
        root = Path(directory)
        (root / '.dummy-secrets').write_text('synthetic-only\n')
        source = root / 'shared/source/base/values.yaml'
        source.parent.mkdir(parents=True)
        source.write_text('PASSWORD: synthetic-release-password\n')
        environment = root / 'shared/source/cluster-1/values.yaml'
        environment.parent.mkdir(parents=True)
        environment.write_text('{}\n')
        app = root / 'apps/app/cluster-1/values.yaml'
        app.parent.mkdir(parents=True)
        app.write_text("ACL: {$template: 'user app on #${shared/source/PASSWORD | sha256} ~* +@all'}\n")
        args = ('resolve', '--input', 'dummy', '--root', str(root), '--projects', 'apps,shared', '--environments', 'cluster-1', '--bundle', 'apps/app/cluster-1')
        expected = {'ACL': 'user app on #' + hashlib.sha256(b'synthetic-release-password').hexdigest() + ' ~* +@all'}
        result = run(binary, 'resolve', '--input', 'memory', '--projects', 'apps,shared',
                     '--environments', 'cluster-1', '--bundle', 'apps/app/cluster-1',
                     input=json.dumps({'shared/source/base/values.yaml': source.read_text(),
                                       'shared/source/cluster-1/values.yaml': '{}\n',
                                       'apps/app/cluster-1/values.yaml': app.read_text()}).encode())
        assert result.returncode == 0 and not result.stderr and json.loads(result.stdout) == expected, 'memory resolver contract mismatch'
        for invalid in (False, True, False):
            source.write_text('PASSWORD: {$ref: MISSING}\n' if invalid else 'PASSWORD: synthetic-release-password\n')
            result = run(binary, *args)
            if invalid:
                assert result.returncode != 0 and not result.stdout and result.stderr, 'invalid candidate emitted output'
                assert b'MISSING' not in result.stderr, 'unsanitized failure'
            else:
                assert result.returncode == 0 and not result.stderr, 'resolver failed'
                assert json.loads(result.stdout) == expected, 'resolve/sha256 contract mismatch'


def check_archives(directory, version, commit):
    directory = Path(directory)
    entries = {}
    for line in (directory / 'checksums.txt').read_text().splitlines():
        digest, name = line.split()
        assert Path(name).name == name and name not in entries, 'invalid checksum entry'
        assert len(digest) == 64 and all(c in '0123456789abcdef' for c in digest)
        entries[name] = digest
    expected = {f'external-secrets-sops_v{version}_{system}_{arch}.{extension}'
                for system, arches, extension in [('linux', ('amd64', 'arm64'), 'tar.gz'),
                                                   ('darwin', ('amd64', 'arm64', 'all'), 'tar.gz'),
                                                   ('windows', ('amd64', 'arm64'), 'zip')]
                for arch in arches}
    assert set(entries) == expected, 'release platform/checksum inventory mismatch'
    native_system = platform.system().lower()
    native_arch = {'x86_64': 'amd64', 'arm64': 'arm64', 'aarch64': 'arm64'}[platform.machine()]
    exercised = False
    for name, digest in entries.items():
        archive = directory / name
        assert hashlib.sha256(archive.read_bytes()).hexdigest() == digest, f'checksum mismatch: {name}'
        if name.endswith('.zip'):
            with zipfile.ZipFile(archive) as stream:
                binaries = [n for n in stream.namelist() if n.endswith('/external-secrets-sops.exe') or n == 'external-secrets-sops.exe']
                assert len(binaries) == 1, 'binary missing or ambiguous'
                payload = stream.read(binaries[0])
        else:
            with tarfile.open(archive) as stream:
                binaries = [n for n in stream.getmembers() if n.isfile() and Path(n.name).name == 'external-secrets-sops']
                assert len(binaries) == 1, 'binary missing or ambiguous'
                payload = stream.extractfile(binaries[0]).read()
        with tempfile.TemporaryDirectory() as scratch:
            binary = Path(scratch) / 'external-secrets-sops'
            binary.write_bytes(payload)
            metadata = subprocess.run(['go', 'version', '-m', str(binary)], capture_output=True, check=True).stdout.decode()
            assert f'vcs.revision={commit}' in metadata and version.encode() in payload and commit.encode() in payload, 'embedded release metadata mismatch'
            if f'_{native_system}_{native_arch}.' in name:
                binary.chmod(0o700)
                check_resolver(binary, version, commit)
                exercised = True
    assert exercised, 'no native platform exercised'


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('directory')
    parser.add_argument('--version', required=True)
    parser.add_argument('--commit', required=True)
    arguments = parser.parse_args()
    check_archives(arguments.directory, arguments.version, arguments.commit)
    print('Release checksums, platforms, metadata and resolve/sha256 contract verified')
