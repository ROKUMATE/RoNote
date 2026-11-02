// Command extract fetches one page with the production fetcher rules and
// prints its IR: JSON by default, or a readable text dump.
//
//	go run ./cmd/extract [--format=json|text] <url>
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"

	"github.com/rokumate/ronote/internal/extract"
	"github.com/rokumate/ronote/internal/fetch"
	"github.com/rokumate/ronote/internal/ir"
)

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "extract:", err)
		os.Exit(1)
	}
}

func run(args []string, out io.Writer) error {
	flags := flag.NewFlagSet("extract", flag.ContinueOnError)
	format := flags.String("format", "json", "output format: json or text")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 1 || (*format != "json" && *format != "text") {
		return errors.New("usage: extract [--format=json|text] <url>")
	}
	pageURL := flags.Arg(0)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	res, err := fetch.New(fetch.Options{}).Fetch(ctx, fetch.Request{URL: pageURL})
	if err != nil {
		return err
	}
	doc, err := extract.Extract(bytes.NewReader(res.Body), ir.Source{
		URL:          pageURL,
		FinalURL:     res.FinalURL,
		FetchedAt:    res.FetchedAt,
		BodySHA256:   res.BodySHA256,
		ETag:         res.ETag,
		LastModified: res.LastModified,
	})
	if err != nil {
		return err
	}
	if err := ir.Validate(doc); err != nil {
		return fmt.Errorf("the extracted document is invalid: %w", err)
	}

	if *format == "text" {
		return writeText(out, doc)
	}
	enc := json.NewEncoder(out)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	return enc.Encode(doc)
}
