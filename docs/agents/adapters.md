# Docs-framework adapters

Read before adding or changing an adapter in `internal/extract/adapter`.

- An adapter is configuration in `internal/extract/adapter`. Transforms such as callouts, tabs, code titles and line-number tables live once, in `internal/extract`. When a framework needs something new, add it there as a config field, not as adapter code.
- `Remove` is the only way an adapter drops content. Every entry gets a comment saying what chrome it is.
- Detection order is generator, then signature, then host, across all adapters; registry order breaks ties.
- Each adapter ships with:
  - at least two golden fixtures. Use a trimmed real excerpt only if the license allows it, recorded in `testdata/golden/SOURCES.md`; otherwise write synthetic HTML with the same DOM and invented text;
  - its own rules in `adapterRules` (`verbatim_test.go`), written apart from its configuration;
  - a page in `live_test.go`.
- After changing an adapter, run `make test-live`. Every page must show its adapter, code found equal to the expected count, a clean verbatim check, zero junk, and only expected warnings. If robots.txt blocks a page, report it; never work around it.
