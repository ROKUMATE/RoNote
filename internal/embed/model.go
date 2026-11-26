package embed

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"time"
)

// ErrNotAnswering is an Ollama that didn't answer: not up yet, or down.
var ErrNotAnswering = errors.New("embed: not answering")

// ModelDigest is the digest Ollama at url lists for model (/api/tags). A
// model it doesn't list is an error, and so, wrapping ErrNotAnswering, is
// an Ollama that doesn't answer.
func ModelDigest(ctx context.Context, client *http.Client, url, model string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url+"/api/tags", http.NoBody)
	if err != nil {
		return "", err
	}
	res, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("%w: %s: %w", ErrNotAnswering, url, err)
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusOK {
		return "", fmt.Errorf("%w: %s answered %d", ErrNotAnswering, url, res.StatusCode)
	}
	var tags struct {
		Models []struct {
			Name   string `json:"name"`
			Digest string `json:"digest"`
		} `json:"models"`
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, maxResponse)).Decode(&tags); err != nil {
		return "", fmt.Errorf("embed: %s's models: %w", url, err)
	}
	for _, m := range tags.Models {
		if m.Name == model {
			return m.Digest, nil
		}
	}
	return "", fmt.Errorf("embed: %s doesn't serve %s", url, model)
}

// SameModel fails unless the Ollamas at a and b serve model with one digest:
// a search compares its query's vector, embedded at one, with documents'
// vectors, embedded at the other, which only the identical model makes
// comparable (docs/adr/0011-retrieval.md).
func SameModel(ctx context.Context, client *http.Client, a, b, model string) error {
	da, err := ModelDigest(ctx, client, a, model)
	if err != nil {
		return err
	}
	db, err := ModelDigest(ctx, client, b, model)
	if err != nil {
		return err
	}
	if da != db {
		return fmt.Errorf("embed: %s at %s is %s, but at %s is %s: queries and documents would be embedded by different models", model, a, da, b, db)
	}
	return nil
}

// ModelMismatch is what the api logs, once, when the two embedders don't
// serve the model with one digest; the health watch alerts on it
// (deploy/alerts/watch.sh).
const ModelMismatch = "embedders serve different models"

// ErrUnchecked is a query embedder whose model isn't confirmed yet: one of
// the two Ollamas hasn't answered.
var ErrUnchecked = errors.New("embed: the embedders' model isn't confirmed yet")

// ErrModelMismatch is a query embedder whose model isn't the indexing one's,
// or that lacks it.
var ErrModelMismatch = errors.New("embed: " + ModelMismatch)

// ErrCold is a query embedder whose model isn't loaded: a search never
// loads it, as a load takes longer than a query may wait, and the query's
// timeout would abort it (docs/adr/0011-retrieval.md).
var ErrCold = errors.New("embed: the query embedder's model isn't loaded yet")

// warmTimeout bounds one load of the query embedder's model, which on 2
// busy cores takes far longer than a query may wait.
const warmTimeout = 10 * time.Minute

// Checked is the query embedder as search uses it (docs/adr/0011-retrieval.md).
// It embeds only once both Ollamas have been seen to serve the model with
// one digest, as a query's vector is compared with documents', and once
// the model is loaded. Until then it refuses, so search gives word matches
// alone, as when Ollama is down, and sends the embedder nothing: Ollama
// loads a model on the first request for it, and a search's 2 s would
// abort a load that takes longer, every search starting it over. Watch
// does the loading, with no such hurry, and does it again when a search
// times out. On a mismatch or a missing model it logs ModelMismatch once
// and stays off until the api restarts. Nothing is compared when both are
// one address. The api never refuses to start over it.
type Checked struct {
	Embedder
	client          *http.Client
	indexURL, model string
	queryURL        string
	logger          *slog.Logger
	// cold wakes Watch when a search finds the model not answering in time.
	cold chan struct{}

	mu       sync.Mutex
	decided  bool
	mismatch error
	warm     bool
}

// NewChecked wraps e, the query embedder at queryURL, against the indexing
// one at indexURL, both serving model.
func NewChecked(e Embedder, client *http.Client, indexURL, queryURL, model string, logger *slog.Logger) *Checked {
	return &Checked{
		Embedder: e, client: client, indexURL: indexURL, queryURL: queryURL, model: model, logger: logger,
		decided: indexURL == queryURL, cold: make(chan struct{}, 1),
	}
}

// Check compares the two Ollamas' digests unless that is decided: nil once
// they match, ErrModelMismatch once they don't, and ErrUnchecked while one
// doesn't answer.
func (c *Checked) Check(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.decided {
		return c.mismatch
	}
	err := SameModel(ctx, c.client, c.indexURL, c.queryURL, c.model)
	switch {
	case err == nil:
		c.decided = true
		c.logger.InfoContext(ctx, "embedders serve one model", "model", c.model)
	case errors.Is(err, ErrNotAnswering):
		return fmt.Errorf("%w: %w", ErrUnchecked, err)
	default:
		c.decided = true
		c.mismatch = fmt.Errorf("%w: %w", ErrModelMismatch, err)
		c.logger.ErrorContext(ctx, ModelMismatch, "error", err.Error(), "index", c.indexURL, "query", c.queryURL)
	}
	return c.mismatch
}

// Warm loads the query embedder's model, with the embedder's own options,
// as a search's would be, so Ollama keeps the runner it loads, within
// warmTimeout.
func (c *Checked) Warm(ctx context.Context) error {
	if err := c.Check(ctx); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, warmTimeout)
	defer cancel()
	start := time.Now()
	if _, err := c.Embedder.Embed(ctx, []string{"warm"}); err != nil {
		return fmt.Errorf("embed: warming the query embedder: %w", err)
	}
	c.mu.Lock()
	c.warm = true
	c.mu.Unlock()
	c.logger.InfoContext(ctx, "query embedder warm", "took_ms", time.Since(start).Milliseconds())
	return nil
}

// Embed embeds texts once the model is confirmed and loaded. A search
// that runs out of time leaves it cold, for Watch to load again.
func (c *Checked) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if err := c.Check(ctx); err != nil {
		return nil, err
	}
	c.mu.Lock()
	warm := c.warm
	c.mu.Unlock()
	if !warm {
		return nil, ErrCold
	}
	vecs, err := c.Embedder.Embed(ctx, texts)
	if err != nil && ctx.Err() != nil {
		c.mu.Lock()
		c.warm = false
		c.mu.Unlock()
		select {
		case c.cold <- struct{}{}:
		default:
		}
	}
	return vecs, err
}

// Watch runs for the api's life: it checks every interval until it can
// decide, so a mismatch is logged, and alerted on, without waiting for a
// search; then it warms the model, and warms it again whenever a search
// finds it cold.
func (c *Checked) Watch(ctx context.Context, interval time.Duration) {
	for {
		attempt, cancel := context.WithTimeout(ctx, interval)
		err := c.Check(attempt)
		cancel()
		switch {
		case errors.Is(err, ErrUnchecked):
		case err != nil:
			return
		default:
			if err := c.Warm(ctx); err != nil {
				c.logger.WarnContext(ctx, "the query embedder didn't warm; trying again", "error", err.Error())
				break
			}
			select {
			case <-ctx.Done():
				return
			case <-c.cold:
				continue
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(interval):
		}
	}
}
