"""Ship licenses of the actual helper module closure, plus pinned local source."""
import json
import shutil
import subprocess
from pathlib import Path
from go_notices import notices, module_dir, HEADER


def write_search_notices(binary, out):
    inventory = subprocess.check_output(['go', 'version', '-m', str(binary)], text=True)
    modules = []
    for line in inventory.splitlines():
        fields = line.strip().split('\t')
        if fields[0] == 'dep' and fields[1] != 'github.com/mediactl/clustarr':
            modules.append((fields[1], fields[2]))
    text, entries = notices(sorted(set(modules)), module_dir)
    text = text.replace(HEADER, 'Go modules compiled into bin/search-helper. Local Clustarr source is shipped in third-party/clustarr.\n')
    (out / 'search-go-modules.txt').write_text(text)
    (out / 'search-go-modules.json').write_text(json.dumps(entries, indent=2) + '\n')
    root = Path(__file__).resolve().parent.parent
    shutil.copytree(root / 'LICENSES/third-party', out / 'search', dirs_exist_ok=True)
