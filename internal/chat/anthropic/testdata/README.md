# Adapter test data

`streams/` holds the Messages API's server-sent events for each case in `anthropic_test.go`, and `requests/` the request the adapter sends for it.

The streams were written by hand from Anthropic's documented event format (the streaming, citations and search-results pages, 2026-11-14), not recorded: there was no API key to record them with. `go test ./internal/chat/anthropic -record` sends each request to the real API with `ANTHROPIC_API_KEY` and records what comes back, at a cost of well under a cent; review that diff like code. `-update` rewrites `requests/` from what the adapter sends.
