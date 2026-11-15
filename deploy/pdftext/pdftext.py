"""RoNote's text-layer reader for code in PDFs (docs/adr/0013-pdf-import.md).

docling finds a PDF's code blocks but joins their lines with spaces. This
reads the text layer inside each code block's box instead: line breaks from
the text layer, and each glyph at the column its position puts it in, so
indentation drawn as space survives. It runs beside docling, from the same
image and as sandboxed; it never runs a model.

POST /v1/code (X-Api-Key): multipart "file" (the PDF) and "boxes", a JSON
list of {"page", "l", "t", "r", "b"} in PDF points, origin bottom left.
Answers {"blocks": [[line, ...], ...]}, one list of lines per box.
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
# How far, in points, a glyph's centre may sit outside a box and still be in it.
SLACK = 2.0


def glyphs(page, box):
    """The glyphs of the text layer whose centres lie in box, in text order,
    as (char, x, baseline, size): x and baseline are the glyph's origin.
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
        cx, cy = (left + right) / 2, (bottom + top) / 2
        if box["l"] - SLACK <= cx <= box["r"] + SLACK and box["b"] - SLACK <= cy <= box["t"] + SLACK:
            x, y = ctypes.c_double(), ctypes.c_double()
            raw.FPDFText_GetCharOrigin(text, i, x, y)
            out.append((ch, x.value, y.value, raw.FPDFText_GetFontSize(text, i)))
    return out


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


def render(lines):
    """Each line as text, every glyph at the column its left edge is in.
    The column width is the code font's advance: the median step between
    neighbouring glyphs of a line."""
    steps = [b[1] - a[1] for line in lines for a, b in zip(line, line[1:]) if b[1] > a[1] and columns(a[0]) == 1]
    if not steps:
        return ["".join(g[0] for g in line) for line in lines]
    width = statistics.median(steps)
    left = min(line[0][1] for line in lines)
    out = []
    for line in lines:
        text, column = "", 0
        for g in line:
            # Each glyph's column comes from where it is, not from counting:
            # a CJK fallback font is rarely exactly two columns wide.
            at = round((g[1] - left) / width)
            if at > column:
                text += " " * (at - column)
            text += g[0]
            column = at + columns(g[0])
        out.append(text)
    return out


def code_blocks(pdf, boxes):
    import pypdfium2 as pdfium

    doc = pdfium.PdfDocument(pdf)
    try:
        blocks = []
        for box in boxes:
            page = doc[int(box["page"]) - 1]
            blocks.append(render(lines_of(glyphs(page, box))))
        return blocks
    finally:
        doc.close()


class Handler(BaseHTTPRequestHandler):
    def do_GET(self):
        if self.path == "/health":
            return self.answer(200, {"status": "ok"})
        self.answer(404, {"error": "not found"})

    def do_POST(self):
        if self.path != "/v1/code":
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
        if "file" not in parts or "boxes" not in parts:
            return self.answer(400, {"error": "file and boxes are required"})
        try:
            blocks = code_blocks(parts["file"], json.loads(parts["boxes"]))
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
