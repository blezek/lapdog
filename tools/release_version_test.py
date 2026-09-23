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

  def make_version(
    self,
    *,
    tag: str | None = None,
    version: str | None = None,
    commits_after_tag: int = 0,
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

      command = [
        "make",
        "--no-print-directory",
        "--silent",
        "-f",
        str(MAKEFILE),
        "print-version",
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
