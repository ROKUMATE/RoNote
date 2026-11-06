"""Runs readers' code for the runner's Python worker (python.worker.ts).

Every block runs in __main__, so blocks share state as a notebook's cells do.
A block runs whole, like a cell, and gives back the repr of its last
expression; a doctest transcript runs one statement at a time and prints each
value, like the REPL; a check runs a transcript's examples through doctest
and gives back each one's verdict.
"""

import linecache
import traceback

import __main__
from pyodide.code import eval_code_async

_cells = 0

_header = "Traceback (most recent call last):\n"


def _cell(source):
    """A file name for source, whose lines tracebacks can then show."""
    global _cells
    _cells += 1
    name = f"<cell {_cells}>"
    linecache.cache[name] = (len(source), None, source.splitlines(True), name)
    return name


def _traceback(error):
    """error's traceback from the reader's code on, without the runner's frames."""
    frame = error.__traceback__
    while frame is not None and not frame.tb_frame.f_code.co_filename.startswith("<cell "):
        frame = frame.tb_next
    return "".join(traceback.format_exception(type(error), error, frame))


def _repr(value):
    try:
        return repr(value)
    except Exception as error:
        return f"<repr failed: {type(error).__name__}: {error}>"


def _raised(error):
    """error as a page shows one an example raises: the header and the exception."""
    return _header + "".join(traceback.format_exception_only(type(error), error))


def _tidy(got):
    """got with any traceback in it cut to its header and exception, as pages show one."""
    at = got.find(_header)
    if at < 0:
        return got
    lines = got[at + len(_header) :].splitlines(True)
    first = next((i for i, line in enumerate(lines) if not line.startswith(" ")), len(lines))
    return got[:at] + _header + "".join(lines[first:])


def check(source):
    """Runs source's examples through doctest in __main__, each compared with
    the output the page shows; returns (source, want, got, outcome) for each."""
    import doctest

    examples = []

    class Collect(doctest.DocTestRunner):
        def report_success(self, out, test, example, got):
            examples.append((example.source, example.want, _tidy(got), "passed"))

        def report_failure(self, out, test, example, got):
            examples.append((example.source, example.want, _tidy(got), "failed"))

        def report_unexpected_exception(self, out, test, example, exc_info):
            examples.append((example.source, example.want, _raised(exc_info[1]), "error"))

    test = doctest.DocTestParser().get_doctest(source, {}, "block", None, 0)
    # The session's own names, not the copy doctest makes, so a check sees
    # what earlier blocks defined and leaves what it defines, as a run does.
    test.globs = __main__.__dict__
    Collect(verbose=False).run(test, out=lambda text: None, clear_globs=False)
    return examples


async def run(source, mode):
    """Runs source as mode says (exec, doctest or check); returns (repr of the
    last value or None, traceback or None, the examples checked or None)."""
    if mode == "check":
        try:
            return None, None, check(source)
        except BaseException as error:
            return None, _traceback(error), None
    if mode == "doctest":
        # Imported here, as it takes about a second, which plain runs never pay.
        import doctest

        parts = [example.source for example in doctest.DocTestParser().get_examples(source)]
    else:
        parts = [source]
    last = None
    for part in parts:
        try:
            value = await eval_code_async(part, __main__.__dict__, filename=_cell(part))
        except BaseException as error:
            return None, _traceback(error), None
        if mode == "doctest":
            if value is not None:
                print(_repr(value))
        elif value is not None:
            last = _repr(value)
    return last, None, None
