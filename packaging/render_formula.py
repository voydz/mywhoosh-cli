#!/usr/bin/env python3
"""Render the tap formula from the four verified release checksums."""
import argparse
import pathlib
import re

PLATFORMS = ("macos-arm64", "macos-x86_64", "linux-x86_64", "linux-aarch64")


def render(version, checksums):
    if not re.fullmatch(r"[0-9]+\.[0-9]+\.[0-9]+", version):
        raise ValueError("version must be a stable semantic version without v")
    for platform in PLATFORMS:
        if not re.fullmatch(r"[a-f0-9]{64}", checksums[platform]):
            raise ValueError(f"invalid SHA-256 for {platform}")
    base = f"https://github.com/voydz/mywhoosh-cli/releases/download/v{version}"
    lines = [
        "class MywhooshCli < Formula",
        '  desc "Download original MyWhoosh activity FIT files from the command-line"',
        '  homepage "https://github.com/voydz/mywhoosh-cli"',
        '  license "MIT"',
    ]
    for os_name, variants in (
        ("macos", (("arm", "arm64"), ("intel", "x86_64"))),
        ("linux", (("intel", "x86_64"), ("arm", "aarch64"))),
    ):
        lines.extend(("", f"  on_{os_name} do"))
        for condition, arch in variants:
            platform = f"{os_name}-{arch}"
            lines.extend((
                f"    on_{condition} do",
                f'      url "{base}/mywhoosh-cli-{version}-{platform}.tar.gz"',
                f'      sha256 "{checksums[platform]}"',
                "    end",
            ))
        lines.append("  end")
    lines.extend((
        "",
        "  def install",
        '    bin.install "mywhoosh"',
        "  end",
        "",
        "  test do",
        '    assert_match "Usage", shell_output("#{bin}/mywhoosh --help")',
        '    assert_match version.to_s, shell_output("#{bin}/mywhoosh version")',
        "  end",
        "end",
        "",
    ))
    return "\n".join(lines)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--version", required=True)
    parser.add_argument("--checksums-dir", type=pathlib.Path, required=True)
    args = parser.parse_args()
    checksums = {
        platform: (args.checksums_dir / f"mywhoosh-cli-{args.version}-{platform}.tar.gz.sha256").read_text().strip()
        for platform in PLATFORMS
    }
    print(render(args.version, checksums), end="")


if __name__ == "__main__":
    main()
