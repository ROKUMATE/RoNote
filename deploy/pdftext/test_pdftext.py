"""Tests of pdftext's arithmetic: lines from baselines, columns from
positions. Run with python3 -m unittest (make hooks-test)."""

import unittest

from pdftext import lines_of, mono_fonts, render

WIDTH = 6.0  # a 10pt monospace font's advance


def glyphs(line, baseline, start=0, size=10.0):
    """The glyphs of line as a monospace font sets it from column start, as
    (char, x, baseline, size, right, mono); spaces are gaps, as in a PDF."""
    out = []
    for col, ch in enumerate(line, start):
        if ch != " ":
            x = 50 + col * WIDTH
            out.append((ch, x, baseline, size, x + WIDTH * 0.8, True))
    return out


def text(lines):
    """render's lines as text alone, for code that is all monospace."""
    out = render(lines)
    assert all(mono for _, mono in out if _), out
    return [t for t, _ in out]


def proportional(line, baseline, size=10.0):
    """line as a proportional font sets it: wide letters, narrow ones, and
    word gaps of a third of the size."""
    out, x = [], 50.0
    for ch in line:
        advance = {"m": 8.0, "w": 7.5, "i": 2.5, "l": 2.5, " ": size / 3}.get(ch, 5.0)
        if ch != " ":
            out.append((ch, x, baseline, size, x + advance * 0.9, False))
        x += advance
    return out


class LinesTest(unittest.TestCase):
    def test_indentation_comes_from_positions(self):
        found = glyphs(">>> if x < 0:", 700) + glyphs("...     x = 0", 688) + glyphs("    return x", 676)
        self.assertEqual(text(lines_of(found)), [">>> if x < 0:", "...     x = 0", "    return x"])

    def test_glyphs_of_a_line_group_by_baseline_whatever_their_order(self):
        # Highlighting draws a line in runs; quotes and = sit high, which
        # baselines ignore.
        line = glyphs("print('Zero')", 700)
        found = line[6:] + glyphs("x = 1", 688) + line[:6]
        self.assertEqual(text(lines_of(found)), ["print('Zero')", "x = 1"])

    def test_inner_runs_of_spaces_stay(self):
        found = glyphs("cat    3", 700) + glyphs("window 6", 688)
        self.assertEqual(text(lines_of(found)), ["cat    3", "window 6"])

    def test_a_block_indented_as_a_whole_starts_at_column_0(self):
        found = glyphs("def f():", 700, start=4) + glyphs("    pass", 688, start=4)
        self.assertEqual(text(lines_of(found)), ["def f():", "    pass"])

    def test_wide_glyphs_take_two_columns(self):
        # A CJK fallback font is rarely exactly two columns wide: what
        # follows is placed by where it is.
        found = [("'", 50, 700, 10, 54, True)]
        x = 50 + WIDTH
        for ch in "景太郎":
            # The fallback font isn't monospace; the line still is.
            found.append((ch, x, 700, 10, x + WIDTH * 1.5, False))
            x += WIDTH * 1.6
        found += [(ch, x + i * WIDTH, 700, 10, x + i * WIDTH + 4, True) for i, ch in enumerate("':")]
        found += [(ch, x + (3 + i) * WIDTH, 700, 10, x + (3 + i) * WIDTH + 4, True) for i, ch in enumerate("'a'")]
        self.assertEqual(text(lines_of(found)), ["'景太郎': 'a'"])

    def test_a_line_with_as_many_gaps_as_neighbours(self):
        # Half the steps here span a space: the advance is the smallest.
        found = glyphs(">>> x = x + 1", 700)
        self.assertEqual(text(lines_of(found)), [">>> x = x + 1"])

    def test_blank_lines_come_from_the_gap_between_baselines(self):
        # A blank line has no glyphs: only the baselines around it show it.
        found = glyphs("import os", 700) + glyphs("app = 1", 676) + glyphs("x = 2", 640) + glyphs("y = 3", 628)
        self.assertEqual(text(lines_of(found)), ["import os", "", "app = 1", "", "", "x = 2", "y = 3"])

    def test_a_font_size_that_isnt_points(self):
        # pdfium reports the size of a font scaled by its text matrix as is:
        # here 30, where the glyphs are 10pt. Widths come from the glyphs.
        found = [(ch, x, y, 30.0, r, m) for ch, x, y, _, r, m in glyphs("from os import path", 700)]
        self.assertEqual(text(lines_of(found)), ["from os import path"])

    def test_one_glyph_lines_need_no_width(self):
        self.assertEqual(text(lines_of([("}", 50, 700, 10, 55, True)])), ["}"])

    def test_a_proportional_font_keeps_its_words(self):
        # Slides set code-like text in proportional fonts: there are no
        # columns to place letters in.
        found = proportional("will slice wisely", 700) + proportional("mill list", 688)
        self.assertEqual(render(lines_of(found)), [("will slice wisely", False), ("mill list", False)])

    def test_a_label_in_a_code_box_says_it_is_prose(self):
        # docling's box around code can take in the label above it.
        found = proportional("Filename: src/main.rs", 712) + glyphs("fn main() {", 700) + glyphs("}", 688)
        self.assertEqual(
            render(lines_of(found)),
            [("Filename: src/main.rs", False), ("fn main() {", True), ("}", True)],
        )

    def test_nothing_found_is_no_lines(self):
        self.assertEqual(render(lines_of([])), [])


class FontsTest(unittest.TestCase):
    @staticmethod
    def page(font, fixed, line, baseline, advances):
        """A line set in font, as page_glyphs gives it."""
        out, x = [], 50.0
        for ch, a in zip(line, advances):
            out.append((ch, x, baseline, 10.0, x + a * 0.8, font, fixed, x + a / 2, baseline + 3))
            x += a
        return out

    def test_monospace_by_flag_or_by_steps(self):
        found = (
            self.page("Courier", True, "cd", 700, [6, 6])
            + self.page("SFTT1000", False, "while n > 0: print(n)" * 2, 680, [5.25] * 42)
            + self.page("Palladio", False, "Iteration is repeating" * 2, 660, [5, 2.5, 4, 6.5] * 11)
        )
        self.assertEqual(mono_fonts(found), {"Courier", "SFTT1000"})


if __name__ == "__main__":
    unittest.main()
