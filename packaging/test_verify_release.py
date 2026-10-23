"""Release integrity checks use small local archives, not synthetic API claims."""
import hashlib
import io
import pathlib
import tarfile
import tempfile
import unittest

from render_formula import PLATFORMS
from verify_release import verify


class ReleaseTests(unittest.TestCase):
    def archive_set(self, directory):
        for platform in PLATFORMS:
            archive = directory / f"mywhoosh-cli-0.1.0-{platform}.tar.gz"
            with tarfile.open(archive, "w:gz") as target:
                for name in ("mywhoosh", "LICENSE", "THIRD_PARTY_NOTICES.md"):
                    member = tarfile.TarInfo(name)
                    member.size = 4
                    target.addfile(member, io.BytesIO(b"test"))
            digest = hashlib.sha256(archive.read_bytes()).hexdigest()
            archive.with_name(archive.name + ".sha256").write_text(digest + "\n")

    def test_accepts_complete_verified_release(self):
        with tempfile.TemporaryDirectory() as directory:
            self.archive_set(pathlib.Path(directory))
            verify("0.1.0", pathlib.Path(directory))

    def test_rejects_changed_archive(self):
        with tempfile.TemporaryDirectory() as directory:
            root = pathlib.Path(directory)
            self.archive_set(root)
            archive = root / "mywhoosh-cli-0.1.0-linux-x86_64.tar.gz"
            archive.write_bytes(archive.read_bytes() + b"changed")
            with self.assertRaisesRegex(ValueError, "checksum"):
                verify("0.1.0", root)

    def test_rejects_missing_platform(self):
        with tempfile.TemporaryDirectory() as directory:
            with self.assertRaises(FileNotFoundError):
                verify("0.1.0", pathlib.Path(directory))


if __name__ == "__main__":
    unittest.main()
