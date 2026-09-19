#!/usr/bin/env python3
"""Exercise release contracts without a remote tag, token or GitHub Release."""

import pathlib
import subprocess
import sys
import tarfile
import tempfile
import unittest

# Importing local tooling must not dirty the source used by the bundle builder.
sys.dont_write_bytecode = True
import release

ROOT = pathlib.Path(__file__).resolve().parent.parent


class ReleaseContractTests(unittest.TestCase):
    """Test public naming/notes and fail-closed version selection."""

    def test_semantic_tags(self):
        for tag in ("v0.0.0", "v1.0.0", "v1.1.0", "v12.34.56"):
            self.assertEqual(release.validate_tag(tag), tag)
        for tag in ("1.0.0", "v1.0", "v01.0.0", "v1.00.0", "v1.0.00", "v1.0.0-rc.1", "v1.0.0+build", "v1.0.0\n", "../v1.0.0"):
            with self.subTest(tag=tag), self.assertRaises(ValueError):
                release.validate_tag(tag)

    def test_notes_match_exact_section(self):
        changelog = "# Changelog\n\n## Unreleased\n\nFuture\n\n## 1.0.0 - 2026-09-18\n\n- Stable.\n\n## 0.9.0 - 2026-09-17\n\nOld\n"
        notes = release.release_notes(changelog, "v1.0.0")
        self.assertIn("- Stable.", notes)
        self.assertIn("https://stef-k.github.io/media-gateway/", notes)
        self.assertNotIn("Future", notes)
        self.assertNotIn("Old", notes)
        for invalid in ("", changelog.replace("1.0.0 -", "1.0.1 -"), changelog.replace("2026-09-18", "2026-02-30"), changelog.replace("- Stable.", ""), changelog + "## 1.0.0 - 2026-09-18\nDuplicate\n"):
            with self.subTest(changelog=invalid), self.assertRaises(ValueError):
                release.release_notes(invalid, "v1.0.0")

    def test_archive_filename(self):
        self.assertEqual(release.archive_stem("v1.0.0") + ".tar.gz", "media-gateway-v1.0.0-linux-amd64.tar.gz")
        with self.assertRaises(ValueError):
            release.archive_stem("v1.0.0/invalid")


class BundleTests(unittest.TestCase):
    """Exercise the actual packager and compiled application."""

    def test_actual_bundles(self):
        """Build both modes from this exact commit, tagging only a disposable clone."""
        with tempfile.TemporaryDirectory(prefix="gateway-release-test-") as directory:
            work = pathlib.Path(directory)
            source = work / "source"
            subprocess.run(["git", "clone", "--quiet", "--no-hardlinks", "--no-tags", str(ROOT), str(source)], check=True)
            revision = release.git(source, "rev-parse", "HEAD")
            builder = source / "scripts/build-bundle.sh"
            normal = work / "normal"
            subprocess.run([str(builder), str(normal)], check=True)
            archive = normal / f"media-gateway-linux-amd64-{revision}.tar.gz"
            extracted = work / "normal-extracted"
            with tarfile.open(archive) as bundle:
                bundle.extractall(extracted, filter="data")
            subprocess.run(["sha256sum", "--check", "--strict", "SHA256SUMS"], cwd=extracted, check=True)
            version = subprocess.check_output([str(extracted / "media-gateway"), "-version"], text=True)
            self.assertIn(f"revision={revision} modified=false go=go1.27.1", version)
            self.assertTrue((extracted / "CHANGELOG.md").is_file())
            # No tag is written to the caller's checkout or any remote.
            subprocess.run(["git", "-c", "user.name=Release test", "-c", "user.email=release@example.invalid", "tag", "-a", "v1.1.0", "-m", "Synthetic release test"], cwd=source, check=True)
            release.validate_source(source, "v1.1.0", "HEAD")
            with self.assertRaises(subprocess.CalledProcessError):
                release.validate_source(source, "v1.1.0", "HEAD^")
            for tag in ("", "invalid", "v1.0.1"):
                result = subprocess.run([str(builder), str(work / "invalid"), "--release", tag], capture_output=True)
                self.assertNotEqual(result.returncode, 0)
            # A valid exact tag without notes must also fail before construction.
            subprocess.run(["git", "tag", "v0.0.0"], cwd=source, check=True)
            with self.assertRaises(ValueError):
                release.validate_source(source, "v0.0.0")
            output = work / "release"
            subprocess.run([str(builder), str(output), "--release", "v1.1.0"], check=True)
            archive = output / "media-gateway-v1.1.0-linux-amd64.tar.gz"
            release.verify_bundle(source, archive, "v1.1.0")
            with tarfile.open(archive) as bundle:
                release_files = {str(pathlib.PurePosixPath(m.name).relative_to(release.archive_stem("v1.1.0"))) for m in bundle.getmembers() if m.isfile()}
            self.assertEqual(release_files, {p.relative_to(extracted).as_posix() for p in extracted.rglob("*") if p.is_file()})
            with self.assertRaisesRegex(ValueError, "filename"):
                release.verify_bundle(source, normal / "wrong-name.tar.gz", "v1.1.0")
            # The qualification archive has valid contents but the wrong release layout.
            wrong_layout = work / archive.name
            wrong_layout.hardlink_to(normal / f"media-gateway-linux-amd64-{revision}.tar.gz")
            with self.assertRaisesRegex(ValueError, "layout"):
                release.verify_bundle(source, wrong_layout, "v1.1.0")
            release.write_checksum(archive)
            subprocess.run(["sha256sum", "--check", "--strict", archive.name + ".sha256"], cwd=output, check=True)
            with self.assertRaises(FileExistsError):
                release.write_checksum(archive)
            result = subprocess.run([str(builder), str(output), "--release", "v1.1.0"], capture_output=True)
            self.assertNotEqual(result.returncode, 0)


if __name__ == "__main__":
    unittest.main()
