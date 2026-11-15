"""RoNote's text-layer reader for PDFs (docs/adr/0013-pdf-import.md).

docling finds a PDF's blocks but joins a code block's lines with spaces, and
its layout model sometimes calls code prose. This reads the text layer
inside each box docling found instead: line breaks from the text layer, each
glyph of a monospace line at the column its position puts it in, so
indentation drawn as space survives, and whether each line is set in a
monospace font. It runs beside docling, from the same image and as
sandboxed; it never runs a model.

POST /v1/inspect (X-Api-Key): multipart "file". Answers {"pages", "chars"},
the page count and how many characters the text layer has, or 422 with
{"error": "password"} for a password-protected PDF and {"error":
"unreadable"} for one pdfium can't open.

POST /v1/lines (X-Api-Key): multipart "file" (the PDF) and "boxes", a JSON
list of {"page", "l", "t", "r", "b"} in PDF points, origin bottom left.
Answers {"blocks": [[{"text", "mono"}, ...], ...]}: each box's lines, and
whether each is set in a monospace font.
"""

import ctypes
import hmac
import json
import os
import statistics
import unicodedata
from email.parser import BytesParser
from email.policy import HTTP
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

MAX_BYTES = 26 * 1024 * 1024
# How far, in points, a glyph's centre may sit above or below a box and still
# be in it; across, it may sit half its font size out, as the last glyph of
# a line often does.
SLACK = 2.0
# The PDF font descriptor's FixedPitch flag.
FIXED_PITCH = 1


def page_glyphs(page):
    """Every glyph of the page's text layer, in text order, as (char, x,
    baseline, size, right, font, fixed, cx, cy): x and baseline are the
    glyph's origin, right its ink's right edge, font its font's name, fixed
    whether the font says it is fixed-pitch, and cx, cy the ink's centre.
    Spaces and the breaks pdfium adds are left out: positions decide both."""
    # pdfium is imported where it is used: the docling image has it, and the
    # unit tests of the arithmetic below run without it.
    import pypdfium2.raw as raw

    text = page.get_textpage()
    out = []
    for i in range(text.count_chars()):
        ch = chr(raw.FPDFText_GetUnicode(text, i))
        if ch.isspace() or raw.FPDFText_IsGenerated(text, i) == 1:
            continue
        left, bottom, right, top = text.get_charbox(i, loose=False)
        x, y = ctypes.c_double(), ctypes.c_double()
        raw.FPDFText_GetCharOrigin(text, i, x, y)
        flags, name = ctypes.c_int(0), ctypes.create_string_buffer(256)
        raw.FPDFText_GetFontInfo(text, i, name, 256, flags)
        out.append((
            ch, x.value, y.value, raw.FPDFText_GetFontSize(text, i), right,
            name.value.decode(errors="replace"), bool(flags.value & FIXED_PITCH),
            (left + right) / 2, (bottom + top) / 2,
        ))
    return out


def mono_fonts(found):
    """The names of the fonts on a page that are monospace: those whose
    descriptor says so, and those whose glyphs, next to each other on a
    line, all stand a whole number of advances apart. TeX's typewriter fonts
    don't set the flag."""
    steps, fixed = {}, set()
    for a, b in zip(found, found[1:]):
        if a[6]:
            fixed.add(a[5])
        if a[5] == b[5] and abs(a[2] - b[2]) < 0.5 and b[1] > a[1] and columns(a[0]) == 1:
            steps.setdefault(a[5], []).append(b[1] - a[1])
    if found and found[-1][6]:
        fixed.add(found[-1][5])
    mono = set(fixed)
    for font, s in steps.items():
        ink = statistics.median(g[4] - g[1] for g in found if g[5] == font)
        s = [x for x in s if x >= ink / 2]
        if len(s) >= 20 and monospace(s, min(s)):
            mono.add(font)
    return mono


def in_box(found, box, mono):
    """The glyphs whose centres lie in box, as (char, x, baseline, size,
    right, mono)."""
    return [
        g[:5] + (g[5] in mono,)
        for g in found
        if box["l"] - g[3] / 2 <= g[7] <= box["r"] + g[3] / 2 and box["b"] - SLACK <= g[8] <= box["t"] + SLACK
    ]


def lines_of(found):
    """Groups glyphs into lines by baseline, top to bottom, each left to
    right. pdfium's own breaks can't be trusted: it breaks between runs of
    differently coloured code on one line."""
    lines = []
    for g in found:
        line = next((l for l in lines if abs(l[0][2] - g[2]) <= g[3] / 2), None)
        if line is None:
            lines.append([g])
        else:
            line.append(g)
    lines.sort(key=lambda l: -l[0][2])
    return [sorted(l, key=lambda g: g[1]) for l in lines]


def columns(ch):
    """How many columns a monospace font gives ch: two for wide CJK."""
    return 2 if unicodedata.east_asian_width(ch) in "WF" else 1


def is_mono(line):
    """Whether most of a line's glyphs are in a monospace font: a CJK
    fallback glyph in a line of code doesn't make it prose."""
    return sum(1 for g in line if g[5]) * 2 > len(line)


def render(lines):
    """Each line as (text, mono). A monospace line has every glyph at the
    column its origin is in, the column width being the font's advance: the
    smallest step between neighbouring glyphs, since a step across spaces
    spans more columns, and steps under half a glyph's ink are marks set over
    a letter. Blank lines between monospace lines come from the gaps between
    baselines. A proportional line has no columns: a word gap becomes one
    space, and indentation can't be known. Both are measured against the
    glyphs' ink, not the font size pdfium reports, which is unscaled when a
    text matrix scales the font."""
    ink = statistics.median([g[4] - g[1] for line in lines for g in line] or [0])
    mono = [is_mono(line) for line in lines]
    steps = [
        b[1] - a[1]
        for line, m in zip(lines, mono)
        if m
        for a, b in zip(line, line[1:])
        if b[1] - a[1] >= ink / 2 and columns(a[0]) == 1
    ]
    width = min(steps, default=0)
    left = min((line[0][1] for line, m in zip(lines, mono) if m), default=0)
    # The line pitch is the smallest step between baselines; a step of n
    # pitches has n - 1 blank lines in it.
    gaps = [a[0][2] - b[0][2] for a, b in zip(lines, lines[1:])]
    pitch = min((g for g in gaps if g >= lines[0][0][3] / 2), default=0) if lines else 0
    out = []
    for i, line in enumerate(lines):
        if i > 0 and pitch and mono[i] and mono[i - 1]:
            out += [("", True)] * max(0, round(gaps[i - 1] / pitch) - 1)
        if mono[i] and width > 0:
            out.append((placed(line, left, width), True))
        else:
            out.append((words(line, ink), mono[i]))
    return out


def placed(line, left, width):
    """A monospace line, each glyph at the column its origin is in. Each
    column comes from where the glyph is, not from counting: a CJK fallback
    font is rarely exactly two columns wide."""
    text, column = "", 0
    for g in line:
        at = round((g[1] - left) / width)
        if at > column:
            text += " " * (at - column)
        text += g[0]
        column = at + columns(g[0])
    return text


def monospace(steps, width):
    """Whether nearly every step is a whole number of advances, as in a
    monospace font."""
    whole = sum(1 for s in steps if abs(s / width - round(s / width)) <= 0.15)
    return whole >= 0.9 * len(steps)


def words(line, ink):
    """A proportional line: a space wherever the gap after a glyph's ink is
    over two fifths of a typical glyph's ink."""
    text = line[0][0]
    for a, b in zip(line, line[1:]):
        if b[1] - a[4] > ink * 0.4:
            text += " "
        text += b[0]
    return text


# pdfium's error code for a password it wasn't given.
PASSWORD_ERROR = 4


def inspect(pdf):
    """The PDF's page count and text-layer character count, or the error
    "password" or "unreadable"."""
    import pypdfium2 as pdfium

    try:
        doc = pdfium.PdfDocument(pdf)
    except pdfium.PdfiumError as err:
        return {"error": "password" if getattr(err, "err_code", None) == PASSWORD_ERROR else "unreadable"}
    try:
        chars = sum(doc[i].get_textpage().count_chars() for i in range(len(doc)))
        return {"pages": len(doc), "chars": chars}
    finally:
        doc.close()


def box_lines(pdf, boxes):
    """Each box's lines, as {"text", "mono"}."""
    import pypdfium2 as pdfium

    doc = pdfium.PdfDocument(pdf)
    try:
        pages = {}
        blocks = []
        for box in boxes:
            n = int(box["page"])
            if n not in pages:
                found = page_glyphs(doc[n - 1])
                pages[n] = (found, mono_fonts(found))
            found, mono = pages[n]
            blocks.append([{"text": t, "mono": m} for t, m in render(lines_of(in_box(found, box, mono)))])
        return blocks
    finally:
        doc.close()


class Handler(BaseHTTPRequestHandler):
    def do_GET(self):
        if self.path == "/health":
            return self.answer(200, {"status": "ok"})
        self.answer(404, {"error": "not found"})

    def do_POST(self):
        if self.path not in ("/v1/lines", "/v1/inspect"):
            return self.answer(404, {"error": "not found"})
        key = os.environ.get("PDFTEXT_API_KEY", "")
        if not key or not hmac.compare_digest(self.headers.get("X-Api-Key", ""), key):
            return self.answer(401, {"error": "unauthorized"})
        size = int(self.headers.get("Content-Length") or 0)
        if size <= 0 or size > MAX_BYTES:
            return self.answer(413, {"error": "too large"})
        body = self.rfile.read(size)
        msg = BytesParser(policy=HTTP).parsebytes(
            b"Content-Type: " + self.headers.get("Content-Type", "").encode() + b"\r\n\r\n" + body
        )
        parts = {p.get_param("name", header="content-disposition"): p.get_payload(decode=True) for p in msg.iter_parts()}
        if "file" not in parts:
            return self.answer(400, {"error": "file is required"})
        if self.path == "/v1/inspect":
            found = inspect(parts["file"])
            return self.answer(422 if "error" in found else 200, found)
        if "boxes" not in parts:
            return self.answer(400, {"error": "boxes are required"})
        try:
            blocks = box_lines(parts["file"], json.loads(parts["boxes"]))
        except Exception as err:  # a PDF pdfium can't read, or bad boxes
            return self.answer(422, {"error": str(err)})
        self.answer(200, {"blocks": blocks})

    def answer(self, status, body):
        data = json.dumps(body).encode()
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(data)))
        self.end_headers()
        self.wfile.write(data)

    def log_message(self, *args):
        pass


if __name__ == "__main__":
    ThreadingHTTPServer(("0.0.0.0", int(os.environ.get("PDFTEXT_PORT", "5002"))), Handler).serve_forever()
