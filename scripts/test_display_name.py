import json
import re
import unittest
from pathlib import Path

import yaml

ROOT = Path(__file__).resolve().parents[1]
# The App is Download Hub; Gopeed names only the bundled engine.
NAMES = {'en': 'Download Hub', 'zh-TW': '下載中心', 'zh-CN': '下载中心', 'ja': 'ダウンロードハブ', 'ko': '다운로드 허브',
         'de': 'Download Hub', 'fr': 'Download Hub', 'es': 'Download Hub', 'it': 'Download Hub'}
ENGINE = {'en': 'Gopeed engine', 'zh-TW': 'Gopeed 引擎', 'zh-CN': 'Gopeed 引擎', 'ja': 'Gopeed エンジン', 'ko': 'Gopeed 엔진',
          'de': 'Gopeed-Engine', 'fr': 'moteur Gopeed', 'es': 'motor Gopeed', 'it': 'motore Gopeed'}
APP_NAME_KEYS = ['gopeed.multi.entryHelp', 'gopeed.destination.appDenied', 'gopeed.error.APP_DENIED',
                 'gopeed.error.source_unsupported', 'gopeed.error.engine_crash_loop']


def info():
    manifest = yaml.safe_load((ROOT / 'dsm-package.yaml').read_text())
    return {'en': {'displayname': manifest['displayname']}, **manifest['i18n']['info']}


class DisplayNameTests(unittest.TestCase):
    def test_package_center_names_the_app_download_hub_in_every_language(self):
        self.assertEqual({lang: entry['displayname'] for lang, entry in info().items()}, NAMES)

    def test_store_text_comes_only_from_the_catalog(self):
        # Descriptions and release notes live in the catalog (--catalog /
        # SYNOPKGLAND_CATALOG); the CLI refuses duplicates in the manifest.
        manifest = yaml.safe_load((ROOT / 'dsm-package.yaml').read_text())
        self.assertNotIn('description', manifest)
        self.assertFalse({'description', 'long_description', 'changelog'} & set(manifest['metadata']))
        for lang, entry in manifest['i18n']['info'].items():
            self.assertNotIn('description', entry, lang)

    def test_messages_say_gopeed_only_for_the_engine(self):
        for lang, engine in ENGINE.items():
            with self.subTest(lang=lang):
                messages = json.loads((ROOT / 'i18n/messages' / f'{lang}.json').read_text())
                for key, text in messages.items():
                    # A title starts with a capital: "Moteur Gopeed".
                    self.assertEqual(text.count('Gopeed'), text.lower().count(engine.lower()), f'{key}: {text}')
                for key in APP_NAME_KEYS:
                    self.assertIn(NAMES[lang], messages[key], key)

    def test_the_full_page_comes_from_the_cli_application_portal(self):
        # The CLI's DSM window opens the App full page with its user, icon and
        # name; the package carries no page of its own that rebuilds ctx.
        manifest = yaml.safe_load((ROOT / 'dsm-package.yaml').read_text())
        self.assertEqual(manifest['portal'], {'alias': 'download-hub'})
        self.assertFalse((ROOT / 'ui/portal.html').exists())

if __name__ == '__main__':
    unittest.main()
