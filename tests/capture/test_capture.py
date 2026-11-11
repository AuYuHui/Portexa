import unittest
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[2] / "tools" / "mt4-capture"))
from types import SimpleNamespace

from capture import belongs_to_mt4


class CaptureScopeTests(unittest.TestCase):
    def test_target_and_delegated_target_are_kept(self):
        executable = "MetaTrader4Terminal"
        for comm, ecomm in [
            (executable, ""),
            (executable[:16], ""),
            (executable[:15], ""),
            ("nsurlsessiond", executable[:16]),
        ]:
            with self.subTest(comm=comm, ecomm=ecomm):
                self.assertTrue(belongs_to_mt4(SimpleNamespace(comm=comm, ecomm=ecomm), executable))

    def test_unrelated_unknown_and_similar_names_are_rejected(self):
        for comm, ecomm in [
            ("MobileSafari", "MobileSafari"),
            ("nsurlsessiond", "OtherApp"),
            ("", ""),
            ("MetaTrader", ""),
            ("MetaTrader5", ""),
        ]:
            with self.subTest(comm=comm, ecomm=ecomm):
                self.assertFalse(belongs_to_mt4(SimpleNamespace(comm=comm, ecomm=ecomm), "MetaTrader4Terminal"))

    def test_missing_executable_does_not_capture_unknown_processes(self):
        self.assertFalse(belongs_to_mt4(SimpleNamespace(comm="", ecomm=""), ""))


if __name__ == "__main__":
    unittest.main()
