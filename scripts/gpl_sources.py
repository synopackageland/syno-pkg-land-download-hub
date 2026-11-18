#!/usr/bin/env python3
"""Build the GPL corresponding-source archives published beside a release.

Usage: python3 scripts/gpl_sources.py OUT_DIR [--repo DIR] [--upstream TAR_GZ]

DIR (default: this repository) must be a clean checkout whose HEAD carries the
release tag. Without --upstream, the Gopeed source archive named in
third-party/gopeed/lock.json is downloaded. Either way its SHA-256 is checked.

Writes three archives and prints their SHA-256 sums:
  gopeed-<engine>-upstream-source.tar.gz         the official upstream archive, unchanged
  gopeed-<engine>-upstream-vendor-source.tar.gz  that archive plus its Go modules in vendor/
  syno-pkg-land-download-hub-<tag>-source.tar.gz `git archive HEAD` plus the Go modules of
                                                 backend/ and search-helper/ in vendor/
The output is reproducible: `git archive` uses the commit time, vendored entries
are sorted with fixed timestamps and gzip headers are empty, so the same tag and
module versions give the same bytes as the published release assets.
"""
import argparse, fnmatch, gzip, hashlib, io, json, shutil, subprocess, sys, tarfile, tempfile, urllib.request
from pathlib import Path

NAME = 'syno-pkg-land-download-hub'
ROOT = Path(__file__).resolve().parent.parent
NOT_SOURCE = {'.rar', '.zip', '.7z', '.tar', '.gz', '.tgz', '.bz2', '.xz', '.lzma', '.zst', '.cab', '.iso', '.dmg', '.jar', '.apk', '.exe', '.dll', '.msi'}
EXCLUDED_NOTE = ('Files left out of vendor/ because they are archives or executables, not source code.\n'
                 'No package embeds them, so `go build -mod=vendor` does not need them.\n\n')
DOWNLOAD_LIMIT = 256 * 1024 * 1024


def embed_patterns(folder):
    for go in folder.glob('*.go'):
        for line in go.read_text(errors='replace').splitlines():
            if line.startswith('//go:embed '):
                yield from (p.strip('"`').removeprefix('all:') for p in line.split()[1:])


def embedded(path, root):
    # A pattern names a file, or a folder whose files are all embedded, relative to the package folder holding the directive.
    for folder in path.parents:
        if not folder.is_relative_to(root):
            return False
        rel = path.relative_to(folder).parts
        for pattern in embed_patterns(folder):
            parts = pattern.split('/')
            if len(parts) <= len(rel) and all(fnmatch.fnmatchcase(a, b) for a, b in zip(rel, parts)):
                return True
    return False


def vendor_archive(module, prefix, target, base=None):
    """Write target: the members of the uncompressed tar `base` (if any), then `go mod vendor` of module under prefix."""
    with tempfile.TemporaryDirectory() as tmp:
        out = Path(tmp) / 'vendor'
        r = subprocess.run(['go', 'mod', 'vendor', '-o', str(out)], cwd=module, capture_output=True, text=True)
        if r.returncode:
            raise RuntimeError(f'go mod vendor failed in {module.name}: ' + r.stderr.strip()[-500:])
        files = sorted(p for p in out.rglob('*') if p.is_file())
        excluded = [p for p in files if p.suffix.lower() in NOT_SOURCE]
        for p in excluded:
            if embedded(p, out):
                raise RuntimeError('Embedded file is not source: ' + p.relative_to(out).as_posix())
        source = tarfile.open(fileobj=io.BytesIO(base)) if base else None
        partial = target.with_name(target.name + '.partial')
        try:
            with partial.open('wb') as raw, gzip.GzipFile(filename='', mode='wb', fileobj=raw, mtime=0) as packed, \
                    tarfile.open(fileobj=packed, mode='w', format=tarfile.PAX_FORMAT,
                                 pax_headers=source.pax_headers if source else {}) as tar:
                for m in source.getmembers() if source else []:
                    tar.addfile(m, source.extractfile(m) if m.isfile() else None)
                for path in (p for p in files if p not in excluded):
                    info = tarfile.TarInfo(prefix + 'vendor/' + path.relative_to(out).as_posix())
                    info.size = path.stat().st_size
                    info.mode = 0o644
                    with path.open('rb') as data:
                        tar.addfile(info, data)
                if excluded:
                    note = (EXCLUDED_NOTE + ''.join('vendor/' + p.relative_to(out).as_posix() + '\n' for p in excluded)).encode()
                    info = tarfile.TarInfo(prefix + 'VENDOR-EXCLUDED.txt')
                    info.size = len(note)
                    info.mode = 0o644
                    tar.addfile(info, io.BytesIO(note))
            partial.replace(target)
        finally:
            partial.unlink(missing_ok=True)
            if source:
                source.close()


def download(url, target):
    partial = target.with_name(target.name + '.partial')
    try:
        with urllib.request.urlopen(url, timeout=30) as src, partial.open('wb') as dst:
            total = 0
            while block := src.read(1024 * 1024):
                total += len(block)
                if total > DOWNLOAD_LIMIT:
                    raise RuntimeError('Upstream archive limit')
                dst.write(block)
        partial.replace(target)
    finally:
        partial.unlink(missing_ok=True)


def build(repo, out, upstream=None):
    repo, out = Path(repo).resolve(), Path(out).resolve()
    if subprocess.run(['git', 'status', '--porcelain'], cwd=repo, capture_output=True, text=True, check=True).stdout.strip():
        raise RuntimeError('Repository has uncommitted changes')
    tag = subprocess.run(['git', 'describe', '--tags', '--exact-match', 'HEAD'], cwd=repo, capture_output=True, text=True)
    if tag.returncode:
        raise RuntimeError('HEAD has no release tag')
    version = tag.stdout.strip()
    lock = json.loads((repo / 'third-party/gopeed/lock.json').read_text())
    engine = lock['version']
    out.mkdir(parents=True, exist_ok=True)
    paths = []
    target = out / f'gopeed-{engine}-upstream-source.tar.gz'
    if upstream:
        shutil.copy(Path(upstream).resolve(), target)
    else:
        download(lock['source']['url'], target)
    if hashlib.sha256(target.read_bytes()).hexdigest() != lock['source']['sha256']:
        target.unlink()
        raise RuntimeError('Upstream source hash mismatch')
    paths.append(target)
    with tempfile.TemporaryDirectory() as tmp:
        with tarfile.open(target) as t:
            t.extractall(tmp, filter='data')
        roots = list(Path(tmp).iterdir())
        if len(roots) != 1 or not roots[0].is_dir():
            raise RuntimeError('Invalid source root')
        vendor = out / f'gopeed-{engine}-upstream-vendor-source.tar.gz'
        vendor_archive(roots[0], roots[0].name + '/', vendor)
        paths.append(vendor)
    target = out / f'{NAME}-{version}-source.tar.gz'
    base = subprocess.run(['git', 'archive', '--format=tar', f'--prefix={NAME}/', 'HEAD'], cwd=repo, check=True, capture_output=True).stdout
    vendor_archive(repo / 'backend', f'{NAME}/backend/', target, base)
    vendor_archive(repo / 'search-helper', f'{NAME}/search-helper/', target, gzip.decompress(target.read_bytes()))
    paths.append(target)
    return paths


def main(argv=None):
    parser = argparse.ArgumentParser(description='Build the GPL corresponding-source archives for a tagged release.')
    parser.add_argument('out', help='output folder')
    parser.add_argument('--repo', default=ROOT, help='clean checkout at the release tag (default: this repository)')
    parser.add_argument('--upstream', help='local copy of the Gopeed source archive named in third-party/gopeed/lock.json')
    args = parser.parse_args(argv)
    try:
        paths = build(args.repo, args.out, args.upstream)
    except RuntimeError as e:
        sys.exit(f'gpl_sources: {e}')
    for p in paths:
        print(hashlib.sha256(p.read_bytes()).hexdigest(), p.name)


if __name__ == '__main__':
    main()
