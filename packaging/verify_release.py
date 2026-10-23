#!/usr/bin/env python3
"""Verify all release archives before publication; never execute their contents."""
import argparse
import hashlib
import pathlib
import tarfile

from render_formula import PLATFORMS


def verify(version, directory):
    for platform in PLATFORMS:
        archive = directory / f"mywhoosh-cli-{version}-{platform}.tar.gz"
        expected = archive.with_name(archive.name + ".sha256").read_text().strip()
        actual = hashlib.sha256(archive.read_bytes()).hexdigest()
        if actual != expected:
            raise ValueError(f"checksum mismatch: {archive.name}")
        with tarfile.open(archive) as source:
            members = source.getmembers()
            if {member.name for member in members} != {"mywhoosh", "LICENSE", "THIRD_PARTY_NOTICES.md"}:
                raise ValueError(f"unexpected archive contents: {archive.name}")
            if len(members) != 3 or any(not member.isfile() for member in members):
                raise ValueError(f"archive must contain three regular files: {archive.name}")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--version", required=True)
    parser.add_argument("--directory", type=pathlib.Path, required=True)
    args = parser.parse_args()
    verify(args.version, args.directory)
    print("Verified all four release archives and checksums.")


if __name__ == "__main__":
    main()
