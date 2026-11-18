#!/usr/bin/env python3
"""Render assets/icon/icon_<size>.png from assets/icon/icon.svg.

icon.svg is upstream Gopeed's logo (ui/flutter/assets/icon/icon.svg of the
locked tag, GPL-3.0-or-later). Run after replacing it and commit the PNGs;
needs rsvg-convert (librsvg). dsm-package.yaml `icon: assets/icon` hands them
to the CLI for Package Center and the DSM window.
"""
import subprocess
from pathlib import Path

ICON = Path(__file__).resolve().parent.parent/'assets/icon'
SIZES = [16, 24, 32, 48, 64, 72, 256]

if __name__ == '__main__':
    for size in SIZES:
        subprocess.run(['rsvg-convert', '-w', str(size), '-h', str(size), str(ICON/'icon.svg'),
                        '-o', str(ICON/f'icon_{size}.png')], check=True)
