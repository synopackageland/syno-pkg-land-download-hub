import os
import re
import subprocess
import tempfile
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
SCRIPT = ROOT / 'scripts' / 'frontend-check.sh'


def env(**extra):
    base = {k: v for k, v in os.environ.items() if k != 'SYNOPKGLAND_DOCKER_SSH'}
    return {**base, **extra}


class FrontendCheckTest(unittest.TestCase):
    def test_runs_the_command_in_frontend_here_without_docker_ssh(self):
        out = subprocess.run(['bash', SCRIPT, 'pwd'], capture_output=True, text=True, env=env(), check=True)
        self.assertEqual(out.stdout.strip(), str(ROOT / 'frontend'))
        self.assertEqual(subprocess.run(['bash', SCRIPT, 'exit 3'], env=env()).returncode, 3)

    def test_runs_the_command_in_the_node_image_on_the_docker_host_in_a_gate_slot_after_pnpm_install(self):
        with tempfile.TemporaryDirectory() as tmp:
            bin_dir = Path(tmp, 'bin')
            bin_dir.mkdir()
            ssh = bin_dir / 'ssh'
            ssh.write_text(f'#!/bin/sh\nshift\nprintf \'%s\\n\' "$*" >>"{tmp}/ssh.log"\ncat >/dev/null\n')
            ssh.chmod(0o755)
            subprocess.run(['bash', SCRIPT, 'pnpm test'], stdin=subprocess.DEVNULL, capture_output=True, check=True,
                           env=env(PATH=f'{bin_dir}:{os.environ["PATH"]}', SYNOPKGLAND_DOCKER_SSH='host',
                                   SYNOPKGLAND_CONTAINER_PREFIX='t-1',
                                   SYNOPKGLAND_DOCKER_NPM_REGISTRY='https://mirror.invalid/'))
            run = next(line for line in Path(tmp, 'ssh.log').read_text().split('\n') if '--name t-1-frontend' in line)
        self.assertRegex(run, r'synopkgland-gate\.slot\$slot\.lock')
        self.assertRegex(run, r'library/node:24\.20\.0-bookworm@sha256:[0-9a-f]{64} ')
        self.assertIn('-e npm_config_registry=https://mirror.invalid/', run)
        self.assertIn('-v synopkgland-cache-npm:/cache/npm ', run)
        # vitest takes half the host's CPUs: the other gate slot has the rest.
        self.assertIn('-e VITEST_MAX_WORKERS=$(($(nproc) / 2)) ', run)
        self.assertRegex(run, r"cd frontend && corepack enable --install-directory /tmp/bin pnpm && pnpm install --frozen-lockfile [^;']*--registry=https://mirror\.invalid/ && pnpm test")


if __name__ == '__main__':
    unittest.main()
