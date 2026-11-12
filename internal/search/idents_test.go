package search

import (
	"slices"
	"testing"
)

func TestIdents(t *testing.T) {
	t.Parallel()
	tests := []struct {
		code string
		want []string
	}{
		{"mux := http.NewServeMux()", []string{"mux", "http.newservemux", "http", "newservemux", "new", "serve"}},
		{"g, ctx := errgroup.WithContext(ctx)", []string{"ctx", "errgroup.withcontext", "errgroup", "withcontext", "with", "context"}},
		{"Error [ERR_MODULE_NOT_FOUND]: Cannot find", []string{"error", "err_module_not_found", "err", "module", "not", "found", "cannot", "find"}},
		{"def __init__(self):", []string{"def", "__init__", "init", "self"}},
		{"git commit --no-verify", []string{"git", "commit", "no-verify", "no", "verify"}},
		{"useEffect(() => {})", []string{"useeffect", "use", "effect"}},
		{"HTTPServer", []string{"httpserver", "http", "server"}},
	}
	for _, tt := range tests {
		if got := Idents(tt.code); !slices.Equal(got, tt.want) {
			t.Errorf("Idents(%q) = %q, want %q", tt.code, got, tt.want)
		}
	}
}

func TestIdentVector(t *testing.T) {
	t.Parallel()
	if got := IdentVector([]string{"http.servemux", "http"}); got != "'http.servemux':1A 'http':2A" {
		t.Errorf("IdentVector() = %q", got)
	}
}

func TestIdentQuery(t *testing.T) {
	t.Parallel()
	tests := []struct {
		q, want string
	}{
		{"ServeMux", "('servemux' | ('serve' & 'mux'))"},
		{"http.ServeMux", "('http.servemux' | ('http' & 'servemux') | ('http' & 'serve' & 'mux'))"},
		{"errgroup.WithContext", "('errgroup.withcontext' | ('errgroup' & 'withcontext') | ('errgroup' & 'with' & 'context'))"},
		{"ERR_MODULE_NOT_FOUND", "('err_module_not_found' | ('err' & 'module' & 'not' & 'found'))"},
		{"__init__", "('__init__')"},
		{"--no-verify", "('no-verify' | ('no' & 'verify'))"},
		{"useEffect", "('useeffect' | ('use' & 'effect'))"},
		{"how do I run things concurrently", ""},
		{"what does ServeMux do", "('servemux' | ('serve' & 'mux'))"},
	}
	for _, tt := range tests {
		if got := IdentQuery(tt.q); got != tt.want {
			t.Errorf("IdentQuery(%q) = %q, want %q", tt.q, got, tt.want)
		}
	}
}
