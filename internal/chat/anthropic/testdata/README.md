# Adapter test data

`streams/` holds the Messages API's server-sent events for each case in `anthropic_test.go`, and `requests/` the request the adapter sends for it.

The streams were recorded from Anthropic's API on 2026-11-15, Claude Haiku 5.5, by `go test ./internal/chat/anthropic -record` with `ANTHROPIC_API_KEY`, at well under a cent; `overloaded.sse` alone is written by hand, since an error can't be had on demand. Recording again changes every answer's words: the tests pin what this recording says, so update them with it, and review that diff like code. `-update` rewrites `requests/` from what the adapter sends.
