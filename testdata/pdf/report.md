# PDF verbatim report

Written by `go test ./internal/docling -update` from the recordings in this directory (docs/adr/0013-pdf-import.md). Each printed golden page's code blocks, as the extractor reads them from its HTML, are looked for in the code mapped from its PDF: docling's blocks, read from the text layer by pdftext. Gate: every block found, none with an other difference.

| PDF | pages | conversion | per page | code blocks | exact | whitespace only | other difference | missing |
|---|---|---|---|---|---|---|---|---|
| real-python-controlflow | 2 | 2.1 s | 1.03 s | 3 | 3 | 0 | 0 | 0 |
| godev-getting-started | 3 | 3.1 s | 1.03 s | 10 | 10 | 0 | 0 | 0 |
| mdbook-rust-data-types | 11 | 12.7 s | 1.16 s | 16 | 6 | 10 | 0 | 0 |
| sphinx-requests-quickstart | 3 | 3.1 s | 1.04 s | 14 | 14 | 0 | 0 | 0 |
| docbook-postgres-select | 2 | 2.6 s | 1.30 s | 13 | 13 | 0 | 0 | 0 |

The real PDFs have no HTML to compare with; their code blocks and conversion times:

| PDF | pages | conversion | per page | code blocks |
|---|---|---|---|---|
| mit-6.0001-lecture-3 | 21 | 20.3 s | 0.97 s | 8 |
| think-python-iteration | 8 | 8.2 s | 1.02 s | 19 |

## real-python-controlflow

| block | first line | result | note |
|---|---|---|---|
| 1 | `>>> x = int(input("Please enter an integer: "))` | exact |  |
| 2 | `>>> # Measure some strings:` | exact |  |
| 3 | `# Create a sample collection` | exact |  |

## godev-getting-started

| block | first line | result | note |
|---|---|---|---|
| 1 | `cd` | exact |  |
| 2 | `cd %HOMEPATH%` | exact |  |
| 3 | `mkdir hello` | exact |  |
| 4 | `$ go mod init example/hello` | exact |  |
| 5 | `package main` | exact |  |
| 6 | `$ go run .` | exact |  |
| 7 | `$ go help` | exact |  |
| 8 | `package main` | exact |  |
| 9 | `$ go mod tidy` | exact |  |
| 10 | `$ go run .` | exact |  |

## mdbook-rust-data-types

| block | first line | result | note |
|---|---|---|---|
| 1 | `let guess: u32 = "42".parse().expect("Not a number!");` | exact |  |
| 2 | `$ cargo build` | exact |  |
| 3 | `fn main() {` | whitespace only |  |
| 4 | `fn main() {` | whitespace only |  |
| 5 | `fn main() {` | whitespace only |  |
| 6 | `fn main() {` | whitespace only |  |
| 7 | `fn main() {` | whitespace only |  |
| 8 | `fn main() {` | whitespace only |  |
| 9 | `fn main() {` | whitespace only |  |
| 10 | `fn main() {` | whitespace only |  |
| 11 | `let months = ["January", "February", "March", "April", "May", "June", "July",` | exact |  |
| 12 | `let a: [i32; 5] = [1, 2, 3, 4, 5];` | exact |  |
| 13 | `let a = [3; 5];` | exact |  |
| 14 | `fn main() {` | whitespace only |  |
| 15 | `use std::io;` | whitespace only |  |
| 16 | `thread 'main' panicked at src/main.rs:19:19:` | exact |  |

## sphinx-requests-quickstart

| block | first line | result | note |
|---|---|---|---|
| 1 | `>>> import requests` | exact |  |
| 2 | `>>> r = requests.get('https://api.github.com/events')` | exact |  |
| 3 | `>>> r = requests.post('https://httpbin.org/post', data={'key': 'value'})` | exact |  |
| 4 | `>>> r = requests.put('https://httpbin.org/put', data={'key': 'value'})` | exact |  |
| 5 | `>>> payload = {'key1': 'value1', 'key2': 'value2'}` | exact |  |
| 6 | `>>> print(r.url)` | exact |  |
| 7 | `>>> payload = {'key1': 'value1', 'key2': ['value2', 'value3']}` | exact |  |
| 8 | `>>> import requests` | exact |  |
| 9 | `>>> r.encoding` | exact |  |
| 10 | `>>> r.content` | exact |  |
| 11 | `>>> from PIL import Image` | exact |  |
| 12 | `>>> import requests` | exact |  |
| 13 | `>>> r = requests.get('https://api.github.com/events', stream=True)` | exact |  |
| 14 | `with open(filename, 'wb') as fd:` | exact |  |

## docbook-postgres-select

| block | first line | result | note |
|---|---|---|---|
| 1 | `SELECT * FROM weather;` | exact |  |
| 2 | `SELECT city, temp_lo, temp_hi, prcp, date FROM weather;` | exact |  |
| 3 | `city      \| temp_lo \| temp_hi \| prcp \|    date` | exact |  |
| 4 | `SELECT city, (temp_hi+temp_lo)/2 AS temp_avg, date FROM weather;` | exact |  |
| 5 | `city      \| temp_avg \|    date` | exact |  |
| 6 | `SELECT * FROM weather` | exact |  |
| 7 | `city      \| temp_lo \| temp_hi \| prcp \|    date` | exact |  |
| 8 | `SELECT * FROM weather` | exact |  |
| 9 | `city      \| temp_lo \| temp_hi \| prcp \|    date` | exact |  |
| 10 | `SELECT * FROM weather` | exact |  |
| 11 | `SELECT DISTINCT city` | exact | inside a block docling merged with its neighbours |
| 12 | `city` | exact | inside a block docling merged with its neighbours |
| 13 | `SELECT DISTINCT city` | exact |  |
