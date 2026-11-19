import pathlib
import unittest

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
        # 上限は対応する下限 (MinSbxVersion、v0.51.0) を下回らないので、別の定数を読み違えたら落ちる値で確かめる
        measured = watch.measured_version(VERSION_FILE.read_text(encoding="utf-8"))
        self.assertGreater(measured, (0, 51, 0))


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
