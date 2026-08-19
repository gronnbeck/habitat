package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"strings"

	"golang.org/x/term"

	"github.com/gronnbeck/habitat/internal/config"
	"github.com/gronnbeck/habitat/internal/store"
)

const adminUsage = `habitat admin — manage the server's projects and accounts

  habitat admin create-project <name>    register a project, print its token
  habitat admin list-projects           list projects
  habitat admin create-user <email>     create an account that can sign in

Run these against the server's database (--db), which on a deployed server
means inside the container.
`

func cmdAdmin(args []string) int {
	if len(args) == 0 {
		fmt.Fprint(os.Stderr, adminUsage)
		return exitInvalid
	}
	sub, rest := args[0], args[1:]
	flags, positional := splitFlags(rest)

	fs := flag.NewFlagSet("admin", flag.ContinueOnError)
	dir := fs.String("dir", ".", "project directory containing habitat.yml")
	db := fs.String("db", "", "SQLite file to operate on")
	if err := fs.Parse(flags); err != nil {
		return exitInvalid
	}
	cfg, err := config.Load(*dir)
	if err != nil {
		return fail("%v", err)
	}
	path := *db
	if path == "" {
		path = cfg.DBPath()
	}

	switch sub {
	case "create-project":
		return adminCreateProject(path, positional)
	case "list-projects":
		return adminListProjects(path)
	case "create-user":
		return adminCreateUser(path, positional)
	default:
		fmt.Fprintf(os.Stderr, "unknown admin command %q\n\n%s", sub, adminUsage)
		return exitInvalid
	}
}

func adminCreateProject(dbPath string, args []string) int {
	if len(args) != 1 {
		return fail("create-project needs exactly one name")
	}
	db, err := store.Open(dbPath)
	if err != nil {
		return frameworkFail("opening %s: %v", dbPath, err)
	}
	defer func() { _ = db.Close() }()

	project, token, err := db.CreateProject(args[0])
	if err != nil {
		return fail("%v", err)
	}
	fmt.Printf("Created project %q (slug: %s)\n\n", project.Name, project.Slug)
	fmt.Printf("  HABITAT_TOKEN=%s\n\n", token)
	// Only the hash is stored, so this is the one and only chance to copy it.
	fmt.Println("This token is shown once and cannot be recovered. Store it now.")
	return exitPassed
}

func adminListProjects(dbPath string) int {
	db, err := store.Open(dbPath)
	if err != nil {
		return frameworkFail("opening %s: %v", dbPath, err)
	}
	defer func() { _ = db.Close() }()

	projects, err := db.ListProjects()
	if err != nil {
		return frameworkFail("%v", err)
	}
	for _, p := range projects {
		fmt.Printf("  %-24s %s\n", p.Slug, p.Name)
	}
	return exitPassed
}

func adminCreateUser(dbPath string, args []string) int {
	if len(args) != 1 {
		return fail("create-user needs exactly one email")
	}
	password, err := readPassword()
	if err != nil {
		return fail("%v", err)
	}
	db, err := store.Open(dbPath)
	if err != nil {
		return frameworkFail("opening %s: %v", dbPath, err)
	}
	defer func() { _ = db.Close() }()

	user, err := db.CreateUser(args[0], password)
	if err != nil {
		return fail("%v", err)
	}
	fmt.Printf("Created %s\n", user.Email)
	return exitPassed
}

// readPassword prompts without echoing when attached to a terminal, and falls
// back to a plain read so this works over `kamal app exec` too.
func readPassword() (string, error) {
	fd := int(os.Stdin.Fd())
	if term.IsTerminal(fd) {
		fmt.Print("Password (min 12 characters): ")
		raw, err := term.ReadPassword(fd)
		fmt.Println()
		return string(raw), err
	}
	fmt.Fprintln(os.Stderr, "reading password from stdin (not a terminal, so it will not be hidden)")
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	return strings.TrimRight(line, "\r\n"), err
}
