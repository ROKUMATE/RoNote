// Package checkconfig is `api check-config` and `worker check-config`: the
// configuration, said without a secret (docs/adr/0014-production-topology.md),
// then whether the blob store's bucket exists and takes a write
// (docs/adr/0015-operations.md). --offline skips the bucket, for checking a
// configuration away from its store, as make prod-check-config does.
package checkconfig

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/rokumate/ronote/internal/blob/s3blob"
	"github.com/rokumate/ronote/internal/config"
)

// timeout bounds the bucket's check: a store that doesn't answer fails it.
const timeout = 15 * time.Second

// Run checks the configuration getenv reads, writing what it found to w.
func Run(ctx context.Context, args []string, getenv func(string) string, w io.Writer) error {
	offline := len(args) == 1 && args[0] == "--offline"
	if len(args) > 0 && !offline {
		return fmt.Errorf("takes nothing or --offline, got %q", strings.Join(args, " "))
	}
	cfg, err := config.Check(getenv, w)
	if err != nil {
		return err
	}
	if offline {
		_, err := fmt.Fprintln(w, "blob store: not checked (--offline)")
		return err
	}
	if err := Bucket(ctx, cfg); err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "blob store: bucket %s takes writes\n", cfg.Blob.Bucket)
	return err
}

// Bucket fails unless cfg's bucket exists and takes a write, naming it: the
// api and the worker check it as they start, so a wrong bucket stops them
// rather than a reader's first upload.
func Bucket(ctx context.Context, cfg config.Config) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return s3blob.New(s3blob.Config(cfg.Blob)).Check(ctx)
}
