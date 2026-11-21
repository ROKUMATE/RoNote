// Package cli picks a command's subcommand: the api's and the worker's,
// such as `api migrate` (docs/adr/0014-production-topology.md).
package cli

import (
	"errors"
	"fmt"
	"slices"
	"strings"
)

// Command runs a subcommand with the arguments after its name.
type Command func(args []string) error

// NoArgs is a Command that takes no arguments.
func NoArgs(f func() error) Command {
	return func(args []string) error {
		if len(args) > 0 {
			return fmt.Errorf("takes no arguments, got %q", strings.Join(args, " "))
		}
		return f()
	}
}

// Run runs the subcommand args names, or def when args is empty.
func Run(args []string, def string, commands map[string]Command) error {
	name, rest := def, args
	if len(args) > 0 {
		name, rest = args[0], args[1:]
	}
	command, ok := commands[name]
	if !ok {
		names := make([]string, 0, len(commands))
		for n := range commands {
			names = append(names, n)
		}
		slices.Sort(names)
		return fmt.Errorf("unknown command %q; want one of %s", name, strings.Join(names, ", "))
	}
	if err := command(rest); err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	return nil
}

// ExitError ends a command with Code, for a caller that tells failures
// apart by exit status, such as `api migrate status` for ronote-deploy.
type ExitError struct {
	Code int
	Err  error
}

func (e *ExitError) Error() string { return e.Err.Error() }

func (e *ExitError) Unwrap() error { return e.Err }

// ExitCode is the status a command's result exits with: 0 for none, an
// ExitError's own code, or 1 for any other error.
func ExitCode(err error) int {
	if err == nil {
		return 0
	}
	if exit, ok := errors.AsType[*ExitError](err); ok {
		return exit.Code
	}
	return 1
}
