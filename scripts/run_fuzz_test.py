import tempfile
import unittest
from pathlib import Path

import run_fuzz


class FuzzManifestTest(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.root = Path(self.temp.name)
        (self.root / "pkg").mkdir()
        (self.root / "pkg" / "fuzz_test.go").write_text(
            'package pkg\nimport "testing"\nfunc FuzzAlpha(f *testing.F) {}\n'
        )
        import subprocess
        subprocess.run(["git", "init", "-q", str(self.root)], check=True)
        subprocess.run(["git", "-C", str(self.root), "add", "pkg/fuzz_test.go"], check=True)
        self.manifest = self.root / "targets.txt"

    def tearDown(self):
        self.temp.cleanup()

    def test_exact_manifest_passes(self):
        self.manifest.write_text("./pkg FuzzAlpha\n")
        self.assertEqual([("./pkg", "FuzzAlpha")], run_fuzz.validate(self.root, self.manifest))

    def test_missing_expected_target_fails(self):
        self.manifest.write_text("./pkg FuzzRenamed\n")
        with self.assertRaises(ValueError) as error:
            run_fuzz.validate(self.root, self.manifest)
        self.assertIn("missing or renamed targets: ./pkg FuzzRenamed", str(error.exception))

    def test_new_target_cannot_be_omitted(self):
        with (self.root / "pkg" / "fuzz_test.go").open("a") as source:
            source.write("func FuzzNew(f *testing.F) {}\n")
        self.manifest.write_text("./pkg FuzzAlpha\n")
        with self.assertRaisesRegex(ValueError, "unlisted fuzz targets.*FuzzNew"):
            run_fuzz.validate(self.root, self.manifest)


if __name__ == "__main__":
    unittest.main()
