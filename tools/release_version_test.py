#!/usr/bin/env python3
"""Verify that Make derives release versions from reachable Git tags."""

from __future__ import annotations

import subprocess
import tempfile
import unittest
from pathlib import Path


MAKEFILE = Path(__file__).resolve().parents[1] / "Makefile"


class ReleaseVersionTest(unittest.TestCase):
  def test_nearest_release_tag_becomes_numeric_version(self):
    self.assertEqual("4.5.6", self.make_version(tag="v4.5.6", commits_after_tag=1))

  def test_untagged_commit_keeps_development_fallback(self):
    self.assertEqual("0.1.0", self.make_version())

  def test_explicit_version_overrides_release_tag(self):
    self.assertEqual("7.8.9", self.make_version(tag="v4.5.6", version="7.8.9"))

  def test_local_version_includes_distance_and_commit(self):
    version = self.make_version(tag="v4.5.6", commits_after_tag=2, target="print-local-version")
    self.assertRegex(version, r"^4\.5\.6-2-g[0-9a-f]+ 4\.5\.6\.2$")

  def test_local_version_marks_a_dirty_tree_without_changing_windows_version(self):
    version = self.make_version(
      tag="v4.5.6",
      commits_after_tag=1,
      dirty=True,
      target="print-local-version",
    )
    self.assertRegex(version, r"^4\.5\.6-1-g[0-9a-f]+-dirty 4\.5\.6\.1$")

  def test_untagged_local_version_has_a_numeric_windows_fallback(self):
    version = self.make_version(target="print-local-version")
    self.assertRegex(version, r"^0\.0\.0-0-g[0-9a-f]+ 0\.0\.0\.0$")

  def test_installer_keeps_descriptive_and_numeric_versions_separate(self):
    with tempfile.TemporaryDirectory() as temp_dir:
      repo = Path(temp_dir)
      (repo / "bundle").touch()
      completed = subprocess.run(
        [
          "make",
          "--no-print-directory",
          "--dry-run",
          "-f",
          str(MAKEFILE),
          "installer",
          "BUNDLE=bundle",
          "VERSION=4.5.6-2-gabc1234-dirty",
          "WINDOWS_VERSION=4.5.6.2",
        ],
        cwd=repo,
        check=True,
        capture_output=True,
        text=True,
      )
      self.assertIn("-DVERSION=4.5.6-2-gabc1234-dirty", completed.stdout)
      self.assertIn("-DNUMERIC_VERSION=4.5.6.2", completed.stdout)
      self.assertIn("lapdog-4.5.6-2-gabc1234-dirty-setup.exe", completed.stdout)

  def make_version(
    self,
    *,
    tag: str | None = None,
    version: str | None = None,
    commits_after_tag: int = 0,
    dirty: bool = False,
    target: str = "print-version",
  ) -> str:
    with tempfile.TemporaryDirectory() as temp_dir:
      repo = Path(temp_dir)
      self.run_command(repo, "git", "init", "--quiet")
      self.run_command(repo, "git", "config", "user.name", "LapDog Test")
      self.run_command(repo, "git", "config", "user.email", "lapdog-test@example.invalid")
      (repo / "tracked.txt").write_text("test\n", encoding="utf-8")
      self.run_command(repo, "git", "add", "tracked.txt")
      self.run_command(repo, "git", "commit", "--quiet", "-m", "Test release version")
      if tag is not None:
        self.run_command(repo, "git", "tag", "-a", tag, "-m", "Test release tag")
      for index in range(commits_after_tag):
        (repo / "tracked.txt").write_text(f"test {index}\n", encoding="utf-8")
        self.run_command(repo, "git", "add", "tracked.txt")
        self.run_command(repo, "git", "commit", "--quiet", "-m", "Advance past release tag")
      if dirty:
        (repo / "tracked.txt").write_text("dirty\n", encoding="utf-8")

      command = [
        "make",
        "--no-print-directory",
        "--silent",
        "-f",
        str(MAKEFILE),
        target,
      ]
      if version is not None:
        command.append(f"VERSION={version}")
      completed = subprocess.run(
        command,
        cwd=repo,
        check=True,
        capture_output=True,
        text=True,
      )
      return completed.stdout.strip()

  @staticmethod
  def run_command(repo: Path, *command: str) -> None:
    subprocess.run(command, cwd=repo, check=True, capture_output=True, text=True)


if __name__ == "__main__":
  unittest.main()
