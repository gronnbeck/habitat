package store

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/gronnbeck/habitat/internal/result"
)

func open(t *testing.T) *Store {
	t.Helper()
	db, err := Open(filepath.Join(t.TempDir(), "h.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestProjectTokenRoundTrip(t *testing.T) {
	db := open(t)
	project, token, err := db.CreateProject("Chikara 2")
	if err != nil {
		t.Fatal(err)
	}
	if project.Slug != "chikara-2" {
		t.Fatalf("expected a url-safe slug, got %q", project.Slug)
	}
	found, err := db.ProjectByToken(token)
	if err != nil || found.ID != project.ID {
		t.Fatalf("token should resolve to its project: %v", err)
	}
	if _, err := db.ProjectByToken("hbt_wrong"); !errors.Is(err, ErrNoProject) {
		t.Fatal("an unknown token must not resolve to anything")
	}
}

func TestLocalProjectIsNotRemotelyWritable(t *testing.T) {
	db := open(t)
	if err := db.EnsureLocalProject(); err != nil {
		t.Fatal(err)
	}
	// Its stored hash is a placeholder no token can hash to, so there is no
	// token that grants write access to locally-recorded runs.
	if _, err := db.ProjectByToken("-"); !errors.Is(err, ErrNoProject) {
		t.Fatal("the local project must not be reachable by any token")
	}
}

func TestRunsAreScopedToTheirProject(t *testing.T) {
	db := open(t)
	if err := db.EnsureLocalProject(); err != nil {
		t.Fatal(err)
	}
	mine, _, err := db.CreateProject("Mine")
	if err != nil {
		t.Fatal(err)
	}
	theirs, _, err := db.CreateProject("Theirs")
	if err != nil {
		t.Fatal(err)
	}
	run := result.Run{ID: "run1", SuiteName: "s", Target: "t", Status: result.StatusPassed}
	if err := db.SaveRunFor(mine.ID, run); err != nil {
		t.Fatal(err)
	}

	owned, err := db.RunBelongsTo(mine.ID, "run1")
	if err != nil || !owned {
		t.Fatal("the owning project should see its own run")
	}
	// Without this, guessing a run id would be enough to read another
	// project's evaluation output.
	owned, err = db.RunBelongsTo(theirs.ID, "run1")
	if err != nil || owned {
		t.Fatal("another project must not own this run")
	}

	runs, err := db.ListRunsFor(theirs.ID, "", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 0 {
		t.Fatalf("listing must be scoped per project, got %d runs", len(runs))
	}
}

func TestLocalRunsDefaultToTheLocalProject(t *testing.T) {
	db := open(t)
	if err := db.EnsureLocalProject(); err != nil {
		t.Fatal(err)
	}
	if err := db.SaveRun(result.Run{ID: "r", SuiteName: "s", Status: result.StatusPassed}); err != nil {
		t.Fatal(err)
	}
	runs, err := db.ListRunsFor(LocalProjectID, "", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 1 {
		t.Fatalf("a plain SaveRun should land in the local project, got %d", len(runs))
	}
}

func TestAuthentication(t *testing.T) {
	db := open(t)
	if _, err := db.CreateUser("ken@example.com", "short"); err == nil {
		t.Fatal("a short password must be rejected")
	}
	user, err := db.CreateUser("Ken@Example.com ", "correct-horse-battery")
	if err != nil {
		t.Fatal(err)
	}
	if user.Email != "ken@example.com" {
		t.Fatalf("email should be normalised, got %q", user.Email)
	}
	if _, err := db.Authenticate("ken@example.com", "wrong"); !errors.Is(err, ErrBadCredentials) {
		t.Fatal("a wrong password must fail")
	}
	if _, err := db.Authenticate("nobody@example.com", "whatever"); !errors.Is(err, ErrBadCredentials) {
		t.Fatal("an unknown address must fail the same way")
	}
	if _, err := db.Authenticate("ken@example.com", "correct-horse-battery"); err != nil {
		t.Fatalf("the right password should succeed: %v", err)
	}
}

func TestSessions(t *testing.T) {
	db := open(t)
	user, err := db.CreateUser("ken@example.com", "correct-horse-battery")
	if err != nil {
		t.Fatal(err)
	}
	token, err := db.CreateSession(user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.UserBySession(token); err != nil {
		t.Fatalf("a fresh session should resolve: %v", err)
	}
	if err := db.DeleteSession(token); err != nil {
		t.Fatal(err)
	}
	if _, err := db.UserBySession(token); err == nil {
		t.Fatal("a signed-out session must stop working")
	}
}
