import json
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch
from search_notices import write_search_notices


class SearchNoticesTest(unittest.TestCase):
    def test_local_clustarr_has_explicit_notices_and_module_closure_is_recorded(self):
        with tempfile.TemporaryDirectory() as folder:
            out = Path(folder)
            inventory = '\tdep\tgithub.com/mediactl/clustarr\tv0.0.0\n\t=>\t../third-party/clustarr\t(devel)\n\tdep\texample.org/feed\tv1.0.0\n'
            with patch('search_notices.subprocess.check_output', return_value=inventory), patch('search_notices.notices', return_value=('notices', [{'path': 'example.org/feed'}])) as collect:
                write_search_notices(out/'helper', out)
            self.assertEqual(collect.call_args[0][0], [('example.org/feed', 'v1.0.0')])
            self.assertTrue((out/'search/Clustarr-GPL-3.0.txt').is_file())
            self.assertEqual(json.loads((out/'search-go-modules.json').read_text())[0]['path'], 'example.org/feed')
