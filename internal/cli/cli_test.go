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
