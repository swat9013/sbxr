import contextlib
import io
import pathlib
import re
import sys
import tempfile
import unittest
from unittest import mock

import sbx_stable_watch as watch

REPO_ROOT = pathlib.Path(__file__).resolve().parent.parent
VERSION_FILE = REPO_ROOT / "internal" / "runtime" / "sbx_version.go"

GO_SOURCE = """
var (
	MinSbxVersion = SbxVersion{Major: 0, Minor: 51, Patch: 0}
	MeasuredSbxVersion = SbxVersion{Major: 0, Minor: 53, Patch: 0}
)
"""
MEASURED = (0, 53, 0)


class ParseVersionTest(unittest.TestCase):
    def test_reads_major_minor_patch(self):
        self.assertEqual(watch.parse_version("v0.54.0"), (0, 54, 0))

    def test_rejects_prerelease_and_malformed_tags(self):
        for tag in ["v0.55.0-rc4", "0.54.0", "v0.54", "v0.54.0.1", "v0.054.0", "v0.+5.0", ""]:
            with self.subTest(tag=tag), self.assertRaises(ValueError):
                watch.parse_version(tag)


class MeasuredVersionTest(unittest.TestCase):
    def test_reads_measured_not_min(self):
        self.assertEqual(watch.measured_version(GO_SOURCE), MEASURED)

    def test_fails_when_constant_is_missing(self):
        with self.assertRaises(ValueError):
            watch.measured_version("var MinSbxVersion = SbxVersion{Major: 0, Minor: 51, Patch: 0}")

    def test_reads_the_real_version_file(self):
        # sbx_version.go の書き方が変わって読めなくなったら、週 1 回の実行を待たずにここで落とす。
        # 読んだ値は、同じ file の下限 (MinSbxVersion) を下回らない (decision/0019)
        source = VERSION_FILE.read_text(encoding="utf-8")
        minimum = re.search(r"MinSbxVersion\s*=\s*SbxVersion\{Major:\s*([0-9]+),\s*Minor:\s*([0-9]+),\s*Patch:\s*([0-9]+)\}", source)
        self.assertIsNotNone(minimum)
        self.assertGreaterEqual(watch.measured_version(source), tuple(int(part) for part in minimum.groups()))


class MainTest(unittest.TestCase):
    def run_main(self, latest_tag, tmp):
        titles = pathlib.Path(tmp) / "titles"
        titles.write_text("", encoding="utf-8")
        body = pathlib.Path(tmp) / "body.md"
        argv = [
            "sbx_stable_watch.py",
            "--latest-tag", latest_tag,
            "--version-file", str(VERSION_FILE),
            "--existing-titles", str(titles),
            "--body-out", str(body),
        ]
        stdout = io.StringIO()
        with mock.patch.object(sys, "argv", argv), contextlib.redirect_stdout(stdout):
            watch.main()
        return stdout.getvalue(), body

    def test_unreadable_tag_fails_the_run_with_error_annotation(self):
        # 読めない版を「新しい版は無い」と読み替えると、監視が黙って止まる
        with tempfile.TemporaryDirectory() as tmp:
            with self.assertRaises(SystemExit) as raised:
                self.run_main("v0.55.0-rc4", tmp)
            self.assertTrue(str(raised.exception.code).startswith("::error::"))
            self.assertFalse((pathlib.Path(tmp) / "body.md").exists())

    def test_newer_stable_prints_title_and_writes_body(self):
        with tempfile.TemporaryDirectory() as tmp:
            stdout, body = self.run_main("v999.0.0", tmp)
            self.assertEqual(stdout, "sbx v999.0.0 を実 sbx で確かめる\n")
            self.assertIn("v999.0.0", body.read_text(encoding="utf-8"))


class NewIssueTitleTest(unittest.TestCase):
    def test_newer_stable_needs_issue(self):
        self.assertEqual(
            watch.new_issue_title("v0.54.0", MEASURED, existing_titles=[]),
            "sbx v0.54.0 を実 sbx で確かめる",
        )

    def test_compares_numerically_not_as_text(self):
        self.assertEqual(
            watch.new_issue_title("v0.100.0", MEASURED, existing_titles=[]),
            "sbx v0.100.0 を実 sbx で確かめる",
        )

    def test_same_or_older_stable_needs_no_issue(self):
        for tag in ["v0.53.0", "v0.52.9"]:
            with self.subTest(tag=tag):
                self.assertIsNone(watch.new_issue_title(tag, MEASURED, existing_titles=[]))

    def test_existing_issue_of_same_version_needs_no_issue(self):
        # open か closed かは呼び出し側が区別せずに題名を渡す
        titles = ["別の issue", "sbx v0.54.0 を実 sbx で確かめる"]
        self.assertIsNone(watch.new_issue_title("v0.54.0", MEASURED, existing_titles=titles))

    def test_issue_of_other_version_does_not_block(self):
        titles = ["sbx v0.54.0 を実 sbx で確かめる"]
        self.assertEqual(
            watch.new_issue_title("v0.54.1", MEASURED, existing_titles=titles),
            "sbx v0.54.1 を実 sbx で確かめる",
        )


if __name__ == "__main__":
    unittest.main()
