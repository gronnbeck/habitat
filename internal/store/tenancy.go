package store

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// LocalProjectID owns runs recorded by a local `habitat run`. Having it means
// the schema has one shape everywhere: single-machine use needs no setup, and
// the server's queries don't need a special case for untenanted rows.
const LocalProjectID = "local"

// SessionLifetime is how long a browser session stays valid.
const SessionLifetime = 30 * 24 * time.Hour

var (
	// ErrNoProject is returned when a token or slug matches nothing.
	ErrNoProject = errors.New("project not found")
	// ErrBadCredentials covers both an unknown email and a wrong password, so
	// the response cannot be used to discover which addresses exist.
	ErrBadCredentials = errors.New("invalid email or password")
)

// Project is one tenant: it owns runs, and its token is what a CLI presents.
type Project struct {
	ID        string    `json:"id"`
	Slug      string    `json:"slug"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
}

// User can sign in to the dashboard.
type User struct {
	ID        string    `json:"id"`
	Email     string    `json:"email"`
	CreatedAt time.Time `json:"created_at"`
}

// CreateProject registers a tenant and returns its ingest token. The token is
// returned exactly once and only its hash is stored — there is deliberately no
// way to read it back later.
func (s *Store) CreateProject(name string) (Project, string, error) {
	slug := Slugify(name)
	if slug == "" {
		return Project{}, "", errors.New("project name must contain a letter or digit")
	}
	token := "hbt_" + randomToken()
	project := Project{ID: randomToken()[:16], Slug: slug, Name: name, CreatedAt: time.Now().UTC()}

	_, err := s.db.Exec(
		`INSERT INTO projects (id, slug, name, token_hash, created_at) VALUES (?,?,?,?,?)`,
		project.ID, project.Slug, project.Name, hashToken(token),
		project.CreatedAt.Format(time.RFC3339Nano),
	)
	if err != nil {
		return Project{}, "", fmt.Errorf("creating project: %w", err)
	}
	return project, token, nil
}

// EnsureLocalProject creates the implicit project local runs belong to. Its
// token hash is unmatchable, so it can never be written to over the network.
func (s *Store) EnsureLocalProject() error {
	_, err := s.db.Exec(
		`INSERT OR IGNORE INTO projects (id, slug, name, token_hash, created_at) VALUES (?,?,?,?,?)`,
		LocalProjectID, LocalProjectID, "Local runs", "-", time.Now().UTC().Format(time.RFC3339Nano),
	)
	return err
}

// ProjectByToken resolves an ingest token to its project.
func (s *Store) ProjectByToken(token string) (Project, error) {
	return s.scanProject(`SELECT id, slug, name, created_at FROM projects WHERE token_hash = ?`, hashToken(token))
}

// ProjectBySlug resolves a URL slug to its project.
func (s *Store) ProjectBySlug(slug string) (Project, error) {
	return s.scanProject(`SELECT id, slug, name, created_at FROM projects WHERE slug = ?`, slug)
}

func (s *Store) scanProject(query string, arg any) (Project, error) {
	var p Project
	var created string
	err := s.db.QueryRow(query, arg).Scan(&p.ID, &p.Slug, &p.Name, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return Project{}, ErrNoProject
	}
	if err != nil {
		return Project{}, err
	}
	p.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	return p, nil
}

// ListProjects returns every tenant, with the local one first.
func (s *Store) ListProjects() ([]Project, error) {
	rows, err := s.db.Query(
		`SELECT id, slug, name, created_at FROM projects ORDER BY (id <> 'local'), name`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var projects []Project
	for rows.Next() {
		var p Project
		var created string
		if err := rows.Scan(&p.ID, &p.Slug, &p.Name, &created); err != nil {
			return nil, err
		}
		p.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
		projects = append(projects, p)
	}
	return projects, rows.Err()
}

// CreateUser registers someone who can sign in.
func (s *Store) CreateUser(email, password string) (User, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" || len(password) < 12 {
		return User{}, errors.New("email is required and the password must be at least 12 characters")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return User{}, err
	}
	user := User{ID: randomToken()[:16], Email: email, CreatedAt: time.Now().UTC()}
	_, err = s.db.Exec(
		`INSERT INTO users (id, email, password_hash, created_at) VALUES (?,?,?,?)`,
		user.ID, user.Email, string(hash), user.CreatedAt.Format(time.RFC3339Nano),
	)
	if err != nil {
		return User{}, fmt.Errorf("creating user: %w", err)
	}
	return user, nil
}

// Authenticate checks an email and password.
func (s *Store) Authenticate(email, password string) (User, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	var user User
	var hash, created string
	err := s.db.QueryRow(
		`SELECT id, email, password_hash, created_at FROM users WHERE email = ?`, email,
	).Scan(&user.ID, &user.Email, &hash, &created)
	if errors.Is(err, sql.ErrNoRows) {
		// Spend the same time as a real comparison would, so response timing
		// does not reveal whether the address exists.
		_ = bcrypt.CompareHashAndPassword([]byte("$2a$10$"+strings.Repeat("x", 53)), []byte(password))
		return User{}, ErrBadCredentials
	}
	if err != nil {
		return User{}, err
	}
	if bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) != nil {
		return User{}, ErrBadCredentials
	}
	user.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	return user, nil
}

// CountUsers reports how many people can sign in. The server uses this to
// refuse to start exposed with nobody able to log in.
func (s *Store) CountUsers() (int, error) {
	var count int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&count)
	return count, err
}

// CreateSession issues a browser session and returns its cookie value.
func (s *Store) CreateSession(userID string) (string, error) {
	token := randomToken()
	now := time.Now().UTC()
	_, err := s.db.Exec(
		`INSERT INTO sessions (token_hash, user_id, created_at, expires_at) VALUES (?,?,?,?)`,
		hashToken(token), userID, now.Format(time.RFC3339Nano),
		now.Add(SessionLifetime).Format(time.RFC3339Nano),
	)
	if err != nil {
		return "", err
	}
	return token, nil
}

// UserBySession resolves a session cookie, rejecting expired sessions.
func (s *Store) UserBySession(token string) (User, error) {
	var user User
	var created, expires string
	err := s.db.QueryRow(
		`SELECT u.id, u.email, u.created_at, s.expires_at
		 FROM sessions s JOIN users u ON u.id = s.user_id
		 WHERE s.token_hash = ?`, hashToken(token),
	).Scan(&user.ID, &user.Email, &created, &expires)
	if err != nil {
		return User{}, ErrBadCredentials
	}
	deadline, err := time.Parse(time.RFC3339Nano, expires)
	if err != nil || time.Now().UTC().After(deadline) {
		return User{}, ErrBadCredentials
	}
	user.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	return user, nil
}

// DeleteSession signs someone out.
func (s *Store) DeleteSession(token string) error {
	_, err := s.db.Exec(`DELETE FROM sessions WHERE token_hash = ?`, hashToken(token))
	return err
}

// ListRunsFor returns a project's runs, newest first.
func (s *Store) ListRunsFor(projectID, suiteName string, limit int) ([]Summary, error) {
	if limit <= 0 {
		limit = 50
	}
	query := `SELECT id, suite_name, target, status, pass_rate, COALESCE(git_sha,''), started_at, finished_at
	          FROM runs WHERE COALESCE(project_id, ?) = ?`
	args := []any{LocalProjectID, projectID}
	if suiteName != "" {
		query += ` AND suite_name = ?`
		args = append(args, suiteName)
	}
	query += ` ORDER BY started_at DESC LIMIT ?`
	args = append(args, limit)

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	return scanSummaries(rows)
}

// RunBelongsTo reports whether a run is this project's. Without it, a run id
// from one project would be readable by another that guessed it.
func (s *Store) RunBelongsTo(projectID, runID string) (bool, error) {
	var owner string
	err := s.db.QueryRow(`SELECT COALESCE(project_id, ?) FROM runs WHERE id = ?`, LocalProjectID, runID).Scan(&owner)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return owner == projectID, nil
}

// SuiteNamesFor lists the suites a project has run.
func (s *Store) SuiteNamesFor(projectID string) ([]string, error) {
	rows, err := s.db.Query(
		`SELECT DISTINCT suite_name FROM runs WHERE COALESCE(project_id, ?) = ? ORDER BY suite_name`,
		LocalProjectID, projectID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		names = append(names, name)
	}
	return names, rows.Err()
}

var slugUnsafe = regexp.MustCompile(`[^a-z0-9]+`)

// Slugify turns a project name into a URL-safe identifier.
func Slugify(name string) string {
	return strings.Trim(slugUnsafe.ReplaceAllString(strings.ToLower(name), "-"), "-")
}

// hashToken stores and compares API and session tokens. These are 256 bits of
// randomness rather than user-chosen secrets, so a fast hash is appropriate —
// there is nothing to brute-force. Passwords use bcrypt instead.
func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func randomToken() string {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		// crypto/rand failing means the platform is broken; a predictable
		// token would be far worse than stopping.
		panic("habitat: cannot read random bytes: " + err.Error())
	}
	return hex.EncodeToString(buf)
}
