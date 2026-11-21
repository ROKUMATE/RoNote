package cli

import (
	"errors"
	"strings"
	"testing"
)

func TestRun(t *testing.T) {
	t.Parallel()
	var ran []string
	commands := map[string]Command{
		"serve":       NoArgs(func() error { ran = append(ran, "serve"); return nil }),
		"migrate":     NoArgs(func() error { return errors.New("no database") }),
		"healthcheck": func(args []string) error { ran = append(ran, "healthcheck "+strings.Join(args, " ")); return nil },
	}
	tests := []struct {
		args    []string
		want    string
		wantErr string
	}{
		{args: nil, want: "serve"},
		{args: []string{"serve"}, want: "serve"},
		{args: []string{"healthcheck", "http://127.0.0.1:8080/healthz"}, want: "healthcheck http://127.0.0.1:8080/healthz"},
		{args: []string{"serve", "now"}, wantErr: `serve: takes no arguments, got "now"`},
		{args: []string{"migrate"}, wantErr: "migrate: no database"},
		{args: []string{"start"}, wantErr: `unknown command "start"; want one of healthcheck, migrate, serve`},
	}
	for _, tt := range tests {
		ran = nil
		err := Run(tt.args, "serve", commands)
		if tt.wantErr != "" {
			if err == nil || err.Error() != tt.wantErr {
				t.Errorf("Run(%q) = %v, want %q", tt.args, err, tt.wantErr)
			}
			continue
		}
		if err != nil || len(ran) != 1 || ran[0] != tt.want {
			t.Errorf("Run(%q) ran %q (err %v), want %q", tt.args, ran, err, tt.want)
		}
	}
}

// A command that ends with an ExitError exits with its code, even when Run
// wraps it in the command's name; any other error exits 1.
func TestExitCode(t *testing.T) {
	t.Parallel()
	commands := map[string]Command{
		"status": NoArgs(func() error { return &ExitError{Code: 3, Err: errors.New("behind")} }),
		"broken": NoArgs(func() error { return errors.New("no database") }),
		"fine":   NoArgs(func() error { return nil }),
	}
	tests := []struct {
		name    string
		want    int
		message string
	}{
		{"status", 3, "status: behind"},
		{"broken", 1, "broken: no database"},
		{"fine", 0, ""},
	}
	for _, tt := range tests {
		err := Run([]string{tt.name}, "fine", commands)
		if got := ExitCode(err); got != tt.want {
			t.Errorf("ExitCode(Run(%q)) = %d, want %d", tt.name, got, tt.want)
		}
		if err != nil && err.Error() != tt.message {
			t.Errorf("Run(%q) = %q, want %q", tt.name, err, tt.message)
		}
	}
}
