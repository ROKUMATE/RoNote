package config

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// unprefixedEnv lists the environment variables Go code may read without the
// RONOTE_ prefix, each with its reason. Every other variable is RONOTE_*, as
// the API key is: a name of its own is a name someone sets in the wrong file.
var unprefixedEnv = map[string]string{}

// envReaders are the functions that read a variable by name: the os ones, and
// the helpers that wrap them (config's getenv, the commands' env and envOr).
var envReaders = map[string]bool{
	"Getenv": true, "LookupEnv": true, "getenv": true, "env": true, "envOr": true,
}

func TestEnvNamesCarryThePrefix(t *testing.T) {
	for name, reason := range unprefixedEnv {
		if strings.TrimSpace(reason) == "" {
			t.Errorf("unprefixedEnv[%q] has no reason", name)
		}
	}

	used := map[string]bool{}
	fset := token.NewFileSet()
	err := filepath.WalkDir("../..", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case "node_modules", ".git", "bin":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return err
		}
		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || len(call.Args) == 0 {
				return true
			}
			var fn string
			switch f := call.Fun.(type) {
			case *ast.Ident:
				fn = f.Name
			case *ast.SelectorExpr:
				fn = f.Sel.Name
			}
			lit, ok := call.Args[0].(*ast.BasicLit)
			if !envReaders[fn] || !ok || lit.Kind != token.STRING {
				return true
			}
			name, err := strconv.Unquote(lit.Value)
			if err != nil || strings.HasPrefix(name, "RONOTE_") {
				return true
			}
			if _, ok := unprefixedEnv[name]; ok {
				used[name] = true
				return true
			}
			t.Errorf("%s: reads %q; environment variables are RONOTE_*, or are listed in unprefixedEnv with a reason", fset.Position(call.Pos()), name)
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for name := range unprefixedEnv {
		if !used[name] {
			t.Errorf("unprefixedEnv[%q] is no longer read; remove it", name)
		}
	}
}
