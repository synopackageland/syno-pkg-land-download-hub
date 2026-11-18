"""Verify SDK-native multi-user launcher metadata without rewriting the SPK."""
import hashlib
import io
import json
import re
import tarfile
from pathlib import Path


def unique_file(archive, name):
    members = [m for m in archive.getmembers() if m.name == name]
    if len(members) != 1 or not members[0].isfile():
        raise ValueError('Expected exactly one regular ' + name)
    return archive.extractfile(members[0]).read()


def read_spk(path):
    raw = Path(path).read_bytes()
    with tarfile.open(fileobj=io.BytesIO(raw), mode='r:*') as outer:
        payload = unique_file(outer, 'package.tgz')
        info = unique_file(outer, 'INFO').decode()
    return payload, info


def verify_spk(path, package_id):
    payload, info = read_spk(path)
    checksum = re.findall(r'^checksum="([a-f0-9]{32})"$', info, re.M)
    if checksum != [hashlib.md5(payload).hexdigest()]:
        raise ValueError('CLI payload checksum mismatch')
    with tarfile.open(fileobj=io.BytesIO(payload), mode='r:*') as inner:
        executor_digest = hashlib.sha256(unique_file(inner, 'executor/executor')).hexdigest()
        anchors = re.findall(r'^synopkgland_executor_sha256=.*$', info, re.M)
        if anchors != [f'synopkgland_executor_sha256="{executor_digest}"']:
            raise ValueError('SDK executor INFO anchor must match the payload exactly once')
        config = json.loads(unique_file(inner, 'ui/config'))
    apps = [v for script in config.values() for v in script.values() if v.get('type') == 'app']
    expected = f'SYNO.SDS.{package_id}.MainWindow'
    if len(apps) != 1 or apps[0].get('appWindow') != expected:
        raise ValueError('Unexpected DSM app registry; refuse to release')
    if apps[0].get('allUsers') is not True:
        raise ValueError('SDK launcher must allow authenticated DSM users')
    if apps[0].get('grantPrivilege') != 'all' or apps[0].get('advanceGrantPrivilege') is not True:
        raise ValueError('SDK launcher must register DSM Application Privileges')
