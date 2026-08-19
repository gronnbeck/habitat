// Command habitat runs evaluation suites against a target and serves the
// results.
//
// The binary is both the CLI and the engine: `habitat run` parses the suite,
// starts the ingest server, launches the runner, grades what comes back, and
// persists it — all in this one process. `habitat serve` is the same binary
// long-running, fronting the same store.
package main

import (
	"fmt"
	"os"
	"strings"
)

// Exit codes, kept distinct so CI can tell a failing suite from a broken one.
const (
	exitPassed       = 0
	exitFailed       = 1
	exitInvalid      = 2
	exitFrameworkErr = 3
)

const usage = `habitat — evaluation engine for non-deterministic code

Usage:
  habitat run [flags] <suite> [-- <runner command>]   run a suite and report
  habitat validate [flags] [suite...]                 validate suites (free)
  habitat list [flags]                                list suites
  habitat show [flags] <run-id>                       print a persisted run
  habitat serve [flags]                               browse runs over HTTP
  habitat admin <command>                             manage projects and accounts

Run "habitat <command> --help" for a command's flags.
`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(exitInvalid)
	}
	os.Exit(dispatch(os.Args[1], os.Args[2:]))
}

func dispatch(command string, args []string) int {
	switch command {
	case "run":
		return cmdRun(args)
	case "validate":
		return cmdValidate(args)
	case "list":
		return cmdList(args)
	case "show":
		return cmdShow(args)
	case "serve":
		return cmdServe(args)
	case "admin":
		return cmdAdmin(args)
	case "help", "--help", "-h":
		fmt.Print(usage)
		return exitPassed
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n%s", command, usage)
		return exitInvalid
	}
}

// splitFlags separates flags from positional arguments so a suite name can be
// written before or after its flags. Go's flag package stops at the first
// positional, which would otherwise make argument order load-bearing.
func splitFlags(args []string) (flags, positional []string) {
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if !strings.HasPrefix(arg, "-") {
			positional = append(positional, arg)
			continue
		}
		flags = append(flags, arg)
		// A flag written as "--name value" consumes the next argument.
		if !strings.Contains(arg, "=") && i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
			if needsValue(arg) {
				i++
				flags = append(flags, args[i])
			}
		}
	}
	return flags, positional
}

// needsValue reports whether a flag takes a separate value. Boolean flags do
// not, and must not swallow the positional that follows them.
func needsValue(flag string) bool {
	name := strings.TrimLeft(flag, "-")
	switch name {
	case "json", "help", "h", "no-persist", "no-push":
		return false
	default:
		return true
	}
}

// splitRunner separates the habitat arguments from the runner command that
// follows a "--" separator.
func splitRunner(args []string) (own, runner []string) {
	for i, arg := range args {
		if arg == "--" {
			return args[:i], args[i+1:]
		}
	}
	return args, nil
}

func fail(format string, args ...any) int {
	fmt.Fprintf(os.Stderr, "habitat: "+format+"\n", args...)
	return exitInvalid
}

func frameworkFail(format string, args ...any) int {
	fmt.Fprintf(os.Stderr, "habitat: "+format+"\n", args...)
	return exitFrameworkErr
}
