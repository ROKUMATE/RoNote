# PDF fixtures

The PDF import's fixtures (docs/adr/0013-pdf-import.md). Each `<name>.docling.json` is what docling-serve and pdftext answered for `<name>.pdf` (`make pdf-record`); each `<name>.ir.json` is the IR it maps to; `report.md` is the PDF verbatim report.

## Printed golden pages

`make pdf-fixtures` prints the extractor golden pages named in `printed.json` with Chromium. Their sources and licences are in `testdata/golden/SOURCES.md`.

## Hostile inputs

Made by `make pdf-fixtures`:

- `not-a-pdf.pdf`: HTML with a .pdf name.
- `encrypted.pdf`: `real-python-controlflow.pdf`, encrypted with a user password by qpdf.
- `pages-301.pdf`: 301 pages of one line each.
- `image-only.pdf`: a picture of a page, with no text layer.
- `timeout.pdf`: the `mdbook-v04-reading-files` golden page, recorded with a 10 ms docling timeout.

## Openly licensed PDFs

Retrieved 2026-11-15.

| File | Source | Licence |
|---|---|---|
| `think-python-iteration.pdf` | Chapter 7, "Iteration" (pages 85–92, cut with qpdf), of *Think Python*, 2nd edition, by Allen B. Downey, Green Tea Press: <http://greenteapress.com/thinkpython2/thinkpython2.pdf> | [CC BY-NC 3.0](https://creativecommons.org/licenses/by-nc/3.0/) |
| `mit-6.0001-lecture-3.pdf` | Lecture 3 slides, "String Manipulation, Guess-and-Check, Approximations, Bisection", of MIT 6.0001 *Introduction to Computer Science and Programming in Python*, Fall 2016, by Ana Bell, Eric Grimson and John Guttag, MIT OpenCourseWare: <https://ocw.mit.edu/courses/6-0001-introduction-to-computer-science-and-programming-in-python-fall-2016/resources/mit6_0001f16_lec3/> | [CC BY-NC-SA 4.0](https://creativecommons.org/licenses/by-nc-sa/4.0/) |
