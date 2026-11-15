"""Tests of pdftext's arithmetic: lines from baselines, columns from
positions. Run with python3 -m unittest (make hooks-test)."""

import unittest

from pdftext import lines_of, render

WIDTH = 6.0  # a 10pt monospace font's advance


def glyphs(line, baseline, start=0, size=10.0, skip=()):
    """The glyphs of line as a monospace font sets it from column start, as
    (char, x, baseline, size); spaces are gaps, as in a PDF."""
    out = []
    for col, ch in enumerate(line, start):
        if ch != " " and col not in skip:
            out.append((ch, 50 + col * WIDTH, baseline, size))
    return out


class LinesTest(unittest.TestCase):
    def test_indentation_comes_from_positions(self):
        found = glyphs(">>> if x < 0:", 700) + glyphs("...     x = 0", 688) + glyphs("    return x", 676)
        self.assertEqual(render(lines_of(found)), [">>> if x < 0:", "...     x = 0", "    return x"])

    def test_glyphs_of_a_line_group_by_baseline_whatever_their_order(self):
        # Highlighting draws a line in runs; quotes and = sit high, which
        # baselines ignore.
        line = glyphs("print('Zero')", 700)
        found = line[6:] + glyphs("x = 1", 688) + line[:6]
        self.assertEqual(render(lines_of(found)), ["print('Zero')", "x = 1"])

    def test_inner_runs_of_spaces_stay(self):
        found = glyphs("cat    3", 700) + glyphs("window 6", 688)
        self.assertEqual(render(lines_of(found)), ["cat    3", "window 6"])

    def test_a_block_indented_as_a_whole_starts_at_column_0(self):
        found = glyphs("def f():", 700, start=4) + glyphs("    pass", 688, start=4)
        self.assertEqual(render(lines_of(found)), ["def f():", "    pass"])

    def test_wide_glyphs_take_two_columns(self):
        # A CJK fallback font is rarely exactly two columns wide: what
        # follows is placed by where it is.
        found = [("'", 50, 700, 10)]
        x = 50 + WIDTH
        for ch in "景太郎":
            found.append((ch, x, 700, 10))
            x += WIDTH * 1.6
        found += [(ch, x + i * WIDTH, 700, 10) for i, ch in enumerate("':")]
        found += [(ch, x + (3 + i) * WIDTH, 700, 10) for i, ch in enumerate("'a'")]
        self.assertEqual(render(lines_of(found)), ["'景太郎': 'a'"])

    def test_one_glyph_lines_need_no_width(self):
        self.assertEqual(render(lines_of([("}", 50, 700, 10)])), ["}"])

    def test_nothing_found_is_no_lines(self):
        self.assertEqual(render(lines_of([])), [])


if __name__ == "__main__":
    unittest.main()
