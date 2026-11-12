package embed

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
)

// batchSize is how many texts one request carries.
const batchSize = 32

// maxResponse bounds one response: 32 vectors of 1024 numbers as JSON.
const maxResponse = 4 << 20

// Client embeds through an OpenAI-compatible /embeddings endpoint: Ollama,
// TEI, or a hosted service.
type Client struct {
	// URL is the API root, such as http://127.0.0.1:11434/v1.
	URL   string
	model string
	// HTTP makes the calls; nil means http.DefaultClient. Callers bound
	// each call with their context.
	HTTP *http.Client
}

// NewClient returns a Client for model at url.
func NewClient(url, model string) *Client {
	return &Client{URL: strings.TrimSuffix(url, "/"), model: model}
}

// Model names the client's model.
func (c *Client) Model() string { return c.model }

// Embed embeds texts, batchSize at a time. A 4xx answer is ErrRejected.
func (c *Client) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, 0, len(texts))
	for start := 0; start < len(texts); start += batchSize {
		batch, err := c.embed(ctx, texts[start:min(start+batchSize, len(texts))])
		if err != nil {
			return nil, err
		}
		out = append(out, batch...)
	}
	return out, nil
}

func (c *Client) embed(ctx context.Context, texts []string) ([][]float32, error) {
	body, err := json.Marshal(map[string]any{"model": c.model, "input": texts})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.URL+"/embeddings", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	client := c.HTTP
	if client == nil {
		client = http.DefaultClient
	}
	res, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("embed: %w", err)
	}
	defer func() { _ = res.Body.Close() }()
	data, err := io.ReadAll(io.LimitReader(res.Body, maxResponse+1))
	switch {
	case err != nil:
		return nil, fmt.Errorf("embed: %w", err)
	case len(data) > maxResponse:
		return nil, fmt.Errorf("embed: answer over %d bytes", maxResponse)
	case res.StatusCode >= 400 && res.StatusCode < 500:
		return nil, fmt.Errorf("%w: %d %s", ErrRejected, res.StatusCode, firstLine(data))
	case res.StatusCode != http.StatusOK:
		return nil, fmt.Errorf("embed: %d %s", res.StatusCode, firstLine(data))
	}
	var parsed struct {
		Data []struct {
			Index     int       `json:"index"`
			Embedding []float32 `json:"embedding"`
		} `json:"data"`
	}
	if err := json.Unmarshal(data, &parsed); err != nil {
		return nil, fmt.Errorf("embed: %w", err)
	}
	if len(parsed.Data) != len(texts) {
		return nil, fmt.Errorf("embed: %d vectors for %d texts", len(parsed.Data), len(texts))
	}
	sort.Slice(parsed.Data, func(i, j int) bool { return parsed.Data[i].Index < parsed.Data[j].Index })
	out := make([][]float32, len(texts))
	for i, d := range parsed.Data {
		if len(d.Embedding) != Dims {
			// The configured model isn't one this schema can store.
			return nil, fmt.Errorf("%w: model %s gives %d dimensions, want %d", ErrRejected, c.model, len(d.Embedding), Dims)
		}
		out[i] = d.Embedding
	}
	return out, nil
}

func firstLine(data []byte) string {
	line, _, _ := strings.Cut(string(data), "\n")
	return strings.TrimSpace(line[:min(len(line), 200)])
}
