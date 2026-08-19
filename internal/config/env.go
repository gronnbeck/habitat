package config

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// EnvFilename holds per-project environment variables — in practice the
// project's `HABITAT_TOKEN`. It is deliberately not `.env`: an app usually has
// one of those already, full of its own secrets, and habitat has no business
// reading it.
//
// Gitignore it. It holds a credential.
const EnvFilename = ".habitat.env"

// loadEnvFile reads EnvFilename from dir into the process environment.
//
// A variable already set in the environment always wins, so an explicit
// `HABITAT_TOKEN=… habitat run` overrides the file rather than being silently
// ignored — the same precedence every other dotenv loader uses, and the one
// people expect when overriding for a one-off.
//
// A missing file is not an error: most projects will not have one.
func loadEnvFile(dir string) error {
	path := filepath.Join(dir, EnvFilename)
	file, err := os.Open(path) // #nosec G304 -- the operator's own project file
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()

	warnIfReadableByOthers(path)

	scanner := bufio.NewScanner(file)
	line := 0
	for scanner.Scan() {
		line++
		key, value, ok := parseEnvLine(scanner.Text())
		if !ok {
			continue
		}
		if key == "" {
			return fmt.Errorf("%s:%d: missing a variable name before `=`", EnvFilename, line)
		}
		if _, set := os.LookupEnv(key); set {
			continue
		}
		if err := os.Setenv(key, value); err != nil {
			return err
		}
	}
	return scanner.Err()
}

// parseEnvLine reads one KEY=VALUE line, tolerating `export` and quotes.
// The bool reports whether the line carried an assignment at all.
func parseEnvLine(raw string) (key, value string, ok bool) {
	text := strings.TrimSpace(raw)
	if text == "" || strings.HasPrefix(text, "#") {
		return "", "", false
	}
	text = strings.TrimPrefix(text, "export ")

	name, rest, found := strings.Cut(text, "=")
	if !found {
		return "", "", false
	}
	return strings.TrimSpace(name), unquote(strings.TrimSpace(rest)), true
}

// unquote strips one matching pair of surrounding quotes, so a value copied
// out of a shell script works unchanged.
func unquote(value string) string {
	if len(value) < 2 {
		return value
	}
	first, last := value[0], value[len(value)-1]
	if first == last && (first == '"' || first == '\'') {
		return value[1 : len(value)-1]
	}
	return value
}

// warnIfReadableByOthers says something when a file holding a token is wider
// open than it should be. It warns rather than refuses: it is the operator's
// machine, and a hard failure over file mode would be worse than the risk.
func warnIfReadableByOthers(path string) {
	info, err := os.Stat(path)
	if err != nil {
		return
	}
	if mode := info.Mode().Perm(); mode&0o077 != 0 {
		fmt.Fprintf(os.Stderr,
			"habitat: %s is readable by other users (mode %04o). It holds a token — consider `chmod 600 %s`.\n",
			path, mode, path)
	}
}
