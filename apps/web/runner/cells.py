"""Runs readers' code for the runner's Python worker (python.worker.ts).

Every block runs in __main__, so blocks share state as a notebook's cells do.
A block runs whole, like a cell, and gives back the repr of its last
expression; a doctest transcript runs one statement at a time and prints each
value, like the REPL.
"""

import linecache
import traceback

import __main__
from pyodide.code import eval_code_async

_cells = 0


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


async def run(source, transcript):
    """Runs source; returns (repr of the last value or None, traceback or None)."""
    if transcript:
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
            return None, _traceback(error)
        if transcript:
            if value is not None:
                print(_repr(value))
        elif value is not None:
            last = _repr(value)
    return last, None
