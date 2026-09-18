#!/usr/bin/env python3
"""Validate immutable release inputs and the existing bundle builder's output."""

import argparse
import datetime
import hashlib
import pathlib
import re
import subprocess
import tarfile
import tempfile


def validate_tag(tag):
    """Accept stable SemVer tags only, including SemVer's leading-zero rule."""
    if not re.fullmatch(r"v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)", tag):
        raise ValueError("release tag must be vMAJOR.MINOR.PATCH without leading zeros")
    return tag


def release_notes(changelog, tag):
    """Extract exactly one dated, nonempty section without adjacent releases."""
    validate_tag(tag)
    sections = re.split(r"^## ", changelog, flags=re.MULTILINE)
    if len(sections) < 2 or sections[0].strip() != "# Changelog" or not sections[1].startswith("Unreleased\n"):
        raise ValueError("CHANGELOG must begin with # Changelog and ## Unreleased")
    matching = [s for s in sections[2:] if s.split("\n", 1)[0].startswith(tag[1:] + " - ")]
    if len(matching) != 1:
        raise ValueError("release requires exactly one matching CHANGELOG section")
    heading, _, body = matching[0].partition("\n")
    date = heading.removeprefix(tag[1:] + " - ")
    if not re.fullmatch(r"[0-9]{4}-[0-9]{2}-[0-9]{2}", date):
        raise ValueError("release section requires an ISO date")
    datetime.date.fromisoformat(date)
    if not body.strip():
        raise ValueError("release section is empty")
    return "## " + heading + "\n\n" + body.strip() + "\n\n[Operator documentation](https://stef-k.github.io/media-gateway/)\n"


def git(root, *args):
    """Read repository facts without shell interpolation."""
    return subprocess.check_output(["git", "-C", str(root), *args], text=True).strip()


def validate_source(root, tag, main_ref=None):
    """Require clean exact-tag source, matching notes and optional main ancestry."""
    validate_tag(tag)
    revision = git(root, "rev-parse", "HEAD")
    if git(root, "rev-parse", "--verify", "refs/tags/" + tag + "^{commit}") != revision:
        raise ValueError("release tag does not resolve to HEAD")
    if git(root, "status", "--porcelain", "--untracked-files=all"):
        raise ValueError("release requires a clean checkout")
    if main_ref:
        subprocess.run(["git", "-C", str(root), "merge-base", "--is-ancestor", revision, main_ref], check=True)
    notes = release_notes((root / "CHANGELOG.md").read_text(), tag)
    return revision, notes


def archive_stem(tag):
    """Derive the sole supported distribution name from the validated tag."""
    return "media-gateway-" + validate_tag(tag) + "-linux-amd64"


def verify_bundle(root, archive, tag):
    """Verify layout, every checksum/source asset and exact binary provenance."""
    revision, _ = validate_source(root, tag)
    stem = archive_stem(tag)
    if archive.name != stem + ".tar.gz":
        raise ValueError("unexpected release archive filename")
    with tempfile.TemporaryDirectory(prefix="gateway-release-verify-") as directory:
        destination = pathlib.Path(directory)
        with tarfile.open(archive, "r:gz") as bundle:
            names = set()
            for member in bundle.getmembers():
                path = pathlib.PurePosixPath(member.name)
                if path.parts[:1] != (stem,) or ".." in path.parts or not (member.isfile() or member.isdir()):
                    raise ValueError("invalid archive layout or member type")
                if member.name in names:
                    raise ValueError("duplicate archive member")
                names.add(member.name)
            bundle.extractall(destination, filter="data")
        payload = destination / stem
        subprocess.run(["sha256sum", "--check", "--strict", "SHA256SUMS"], cwd=payload, check=True)
        files = {p.relative_to(payload).as_posix() for p in payload.rglob("*") if p.is_file()}
        manifest = (payload / "SHA256SUMS").read_text().splitlines()
        covered = [line.split("  ./", 1)[1] for line in manifest]
        if len(covered) != len(set(covered)) or set(covered) != files - {"SHA256SUMS"}:
            raise ValueError("internal checksums must cover every packaged file exactly once")
        # Renamed assets are the only mapping; docs and other files keep source paths.
        sources = {"README.md": "deploy/README.bundle.md", "smoke-deployment.py": "scripts/smoke-deployment.py"}
        for name in files - {"media-gateway", "SHA256SUMS"}:
            source = sources.get(name, name)
            if name in ("config.toml.example", "media-gateway.service", "nginx.conf"):
                source = "deploy/" + name
            if (payload / name).read_bytes() != (root / source).read_bytes():
                raise ValueError("packaged asset differs from tagged source: " + name)
        expected = f"media-gateway version={tag} revision={revision} modified=false go=go1.27.1\n"
        actual = subprocess.check_output([str(payload / "media-gateway"), "-version"], text=True)
        if actual != expected:
            raise ValueError("release binary provenance mismatch: " + actual.strip())
        print(actual, end="")


def write_checksum(archive):
    """Create a separate archive checksum, refusing to replace an existing asset."""
    with archive.open("rb") as stream:
        digest = hashlib.file_digest(stream, "sha256").hexdigest()
    with archive.with_name(archive.name + ".sha256").open("x") as stream:
        stream.write(digest + "  " + archive.name + "\n")


def main():
    """Expose validation/notes and post-build verification to Bash and Actions."""
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("command", choices=("validate", "verify"))
    parser.add_argument("tag")
    parser.add_argument("--main-ref")
    parser.add_argument("--archive", type=pathlib.Path)
    args = parser.parse_args()
    root = pathlib.Path(__file__).resolve().parent.parent
    try:
        _, notes = validate_source(root, args.tag, args.main_ref)
        if args.command == "validate":
            print(notes, end="")
        else:
            if args.archive is None:
                parser.error("verify requires --archive")
            verify_bundle(root, args.archive, args.tag)
            write_checksum(args.archive)
    except (ValueError, OSError, subprocess.CalledProcessError, tarfile.TarError) as error:
        parser.exit(1, f"Release validation failed: {error}\n")


if __name__ == "__main__":
    main()
