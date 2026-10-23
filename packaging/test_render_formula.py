"""Packaging contract tests; never require account credentials."""
import pathlib
import subprocess
import tempfile
import unittest

from render_formula import PLATFORMS, render


class FormulaTests(unittest.TestCase):
    def test_each_supported_platform_has_its_own_checksum(self):
        checksums = {platform: str(i) * 64 for i, platform in enumerate(PLATFORMS, 1)}
        formula = render("0.1.0", checksums)
        for platform, digest in checksums.items():
            self.assertIn(f"mywhoosh-cli-0.1.0-{platform}.tar.gz", formula)
            self.assertIn(f'sha256 "{digest}"', formula)
        self.assertEqual(formula.count("      url "), 4)
        self.assertIn('license "MIT"', formula)
        self.assertIn('bin.install "mywhoosh"', formula)

    def test_invalid_version_and_checksum_are_rejected(self):
        checksums = dict.fromkeys(PLATFORMS, "a" * 64)
        for version in ("v0.1.0", "0.1.0\n", '0.1.0"', "dev"):
            with self.assertRaises(ValueError):
                render(version, checksums)
        checksums[PLATFORMS[0]] = "not-a-checksum"
        with self.assertRaises(ValueError):
            render("0.1.0", checksums)

    def test_script_reads_all_four_files(self):
        script = pathlib.Path(__file__).with_name("render_formula.py")
        with tempfile.TemporaryDirectory() as directory:
            for platform in PLATFORMS:
                (pathlib.Path(directory) / f"mywhoosh-cli-0.1.0-{platform}.tar.gz.sha256").write_text("b" * 64 + "\n")
            result = subprocess.run(
                ["python3", str(script), "--version", "0.1.0", "--checksums-dir", directory],
                capture_output=True, text=True, check=True,
            )
        self.assertTrue(result.stdout.startswith("class MywhooshCli < Formula\n"))
        self.assertEqual(result.stderr, "")


if __name__ == "__main__":
    unittest.main()
