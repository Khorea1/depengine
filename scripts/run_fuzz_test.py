import tempfile
import unittest
from pathlib import Path

import run_fuzz


class FuzzManifestTest(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.root = Path(self.temp.name)
        (self.root / "go.mod").write_text("module example.test/fuzzfixture\n\ngo 1.23\n")
        (self.root / "pkg").mkdir()
        self.source = self.root / "pkg" / "fuzz_test.go"
        self.source.write_text(
            'package pkg\nimport "testing"\n'
            '// func FuzzCommentedFake(f *testing.F) {}\n'
            'func FuzzAlpha(seed *testing.F) {}\n'
        )
        self.manifest = self.root / "targets.txt"

    def tearDown(self):
        self.temp.cleanup()

    def test_runtime_discovery_uses_go_signature_and_ignores_comments(self):
        self.manifest.write_text("./pkg FuzzAlpha\n")
        self.assertEqual([("./pkg", "FuzzAlpha")], run_fuzz.validate(self.root, self.manifest))

    def test_root_package_uses_dot_path(self):
        (self.root / "fuzz_test.go").write_text(
            'package fuzzfixture\nimport "testing"\nfunc FuzzRoot(seed *testing.F) {}\n'
        )
        self.manifest.write_text(". FuzzRoot\n./pkg FuzzAlpha\n")
        self.assertEqual(
            [(".", "FuzzRoot"), ("./pkg", "FuzzAlpha")],
            run_fuzz.validate(self.root, self.manifest),
        )

    def test_missing_runtime_target_fails(self):
        self.manifest.write_text("./pkg FuzzMissing\n")
        with self.assertRaisesRegex(ValueError, "missing or renamed targets.*FuzzMissing"):
            run_fuzz.validate(self.root, self.manifest)

    def test_new_runtime_target_cannot_be_omitted(self):
        with self.source.open("a") as source:
            source.write("func FuzzNew(seed *testing.F) {}\n")
        self.manifest.write_text("./pkg FuzzAlpha\n")
        with self.assertRaisesRegex(ValueError, "unlisted fuzz targets.*FuzzNew"):
            run_fuzz.validate(self.root, self.manifest)

    def test_build_tagged_target_is_tracked_without_being_runnable(self):
        self.source.write_text(
            '//go:build never\n\npackage pkg\nimport "testing"\n'
            'func FuzzTagged(seed *testing.F) {}\n'
        )
        self.manifest.write_text("./pkg FuzzTagged\n")
        self.assertEqual([("./pkg", "FuzzTagged")], run_fuzz.validate(self.root, self.manifest))
        self.assertNotIn(("./pkg", "FuzzTagged"), run_fuzz.discover_targets(self.root))

    def test_build_tagged_target_cannot_be_omitted(self):
        self.source.write_text(
            '//go:build never\n\npackage pkg\nimport "testing"\n'
            'func FuzzTagged(seed *testing.F) {}\n'
        )
        self.manifest.write_text("# omitted\n")
        with self.assertRaisesRegex(ValueError, "unlisted fuzz targets.*FuzzTagged"):
            run_fuzz.validate(self.root, self.manifest)

    def test_empty_manifest_and_discovery_fail(self):
        self.source.write_text('package pkg\n')
        self.manifest.write_text("# no targets\n")
        with self.assertRaisesRegex(ValueError, "no fuzz targets discovered"):
            run_fuzz.validate(self.root, self.manifest)


if __name__ == "__main__":
    unittest.main()
