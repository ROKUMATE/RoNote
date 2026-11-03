package canon

import (
	"errors"
	"strings"
	"testing"
)

func TestNormalize(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in, want string
		wantErr  error
	}{
		{in: "https://go.dev/doc/effective_go", want: "https://go.dev/doc/effective_go"},
		{in: "HTTPS://Go.Dev/Doc/Effective_Go", want: "https://go.dev/Doc/Effective_Go"},
		{in: "https://go.dev:443/doc", want: "https://go.dev/doc"},
		{in: "http://go.dev:80/doc", want: "http://go.dev/doc"},
		{in: "http://go.dev:443/doc", want: "http://go.dev:443/doc"},
		{in: "https://localhost:8443/doc", want: "https://localhost:8443/doc"},
		{in: "https://go.dev", want: "https://go.dev/"},
		{in: "https://go.dev/doc#intro", want: "https://go.dev/doc"},
		{in: "https://go.dev/doc?", want: "https://go.dev/doc"},
		{in: "  https://go.dev/doc  ", want: "https://go.dev/doc"},
		{in: "https://go.dev/doc/a%2Fb", want: "https://go.dev/doc/a%2Fb"},
		{
			in:   "https://docs.example.com/p?b=2&utm_source=x&a=1&UTM_Medium=y&gclid=g&fbclid=f&utm=keep#top",
			want: "https://docs.example.com/p?b=2&a=1&utm=keep",
		},
		{in: "https://docs.example.com/p?q=a%20b&q=c+d", want: "https://docs.example.com/p?q=a%20b&q=c+d"},
		{in: "https://docs.example.com/p?utm_source=x", want: "https://docs.example.com/p"},
		{in: "https://[2001:DB8::1]:443/x", want: "https://[2001:db8::1]/x"},
		{in: "ftp://go.dev/doc", wantErr: ErrScheme},
		{in: "javascript:alert(1)", wantErr: ErrInvalid},
		{in: "/doc/effective_go", wantErr: ErrInvalid},
		{in: "https://ada@go.dev/", wantErr: ErrInvalid},
		{in: "https://go.dev/%zz", wantErr: ErrInvalid},
		{in: "https://go.dev/" + strings.Repeat("a", MaxLen), wantErr: ErrTooLong},
	}
	for _, tt := range tests {
		t.Run(tt.in[:min(len(tt.in), 60)], func(t *testing.T) {
			t.Parallel()
			got, err := Normalize(tt.in)
			if !errors.Is(err, tt.wantErr) || got != tt.want {
				t.Errorf("Normalize(%q) = %q, %v; want %q, %v", tt.in, got, err, tt.want, tt.wantErr)
			}
		})
	}
}

func TestResolve(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, final, canonical, want string
	}{
		{name: "no canonical", final: "https://go.dev/doc/?utm_source=x", want: "https://go.dev/doc/"},
		{name: "same host is trusted", final: "https://go.dev/doc/effective_go.html", canonical: "https://go.dev/doc/effective_go", want: "https://go.dev/doc/effective_go"},
		{name: "same host in another case", final: "https://go.dev/a", canonical: "https://GO.DEV/b#x", want: "https://go.dev/b"},
		{
			name: "another host is ignored", final: "https://evil.example/copy", canonical: "https://go.dev/doc/effective_go",
			want: "https://evil.example/copy",
		},
		{name: "a subdomain is another host", final: "https://docs.example.com/a", canonical: "https://example.com/a", want: "https://docs.example.com/a"},
		{name: "www is another host", final: "https://example.com/a", canonical: "https://www.example.com/a", want: "https://example.com/a"},
		{name: "another port is another host", final: "https://example.com/a", canonical: "https://example.com:8443/a", want: "https://example.com/a"},
		{name: "an invalid canonical is ignored", final: "https://go.dev/a", canonical: "javascript:void(0)", want: "https://go.dev/a"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := Resolve(tt.final, tt.canonical)
			if err != nil || got != tt.want {
				t.Errorf("Resolve(%q, %q) = %q, %v; want %q", tt.final, tt.canonical, got, err, tt.want)
			}
		})
	}
	if _, err := Resolve("ftp://go.dev/", ""); !errors.Is(err, ErrScheme) {
		t.Errorf("Resolve() with an ftp final URL err = %v, want ErrScheme", err)
	}
}
