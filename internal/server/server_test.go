package server

import "testing"

func TestIsLoopback(t *testing.T) {
	// This decides whether the server demands a login. Getting it wrong in the
	// permissive direction publishes every stored run, so it is worth pinning.
	local := []string{"127.0.0.1:7878", "localhost:7878", "[::1]:7878", "127.0.0.1"}
	for _, addr := range local {
		if !IsLoopback(addr) {
			t.Errorf("%q should be loopback", addr)
		}
	}
	exposed := []string{"0.0.0.0:7878", ":7878", "[::]:7878", "192.168.1.10:7878", "habitat.np.lol:443"}
	for _, addr := range exposed {
		if IsLoopback(addr) {
			t.Errorf("%q must not count as loopback", addr)
		}
	}
}

func TestSignupTokenMatching(t *testing.T) {
	// The token is the entire gate on account creation, so near-misses must
	// not pass — particularly a prefix, which a naive comparison could allow.
	s := &Server{cfg: Config{SignupToken: "s3cret-token"}}
	if !s.tokenMatches("s3cret-token") {
		t.Fatal("the exact token must match")
	}
	for _, wrong := range []string{"", "s3cret", "s3cret-token ", "S3CRET-TOKEN", "s3cret-token-extra"} {
		if s.tokenMatches(wrong) {
			t.Errorf("%q must not match", wrong)
		}
	}
	// With no token configured, signup is closed — an empty submission must
	// not be treated as a match against an empty secret.
	closed := &Server{cfg: Config{}}
	if closed.cfg.SignupOpen() {
		t.Fatal("signup must be closed when no token is set")
	}
}

func TestIsLocalPath(t *testing.T) {
	// The post-login redirect comes from a query parameter, so it must not be
	// usable to bounce someone to another site.
	for _, path := range []string{"/", "/projects/x", "/projects/x/runs/1"} {
		if !isLocalPath(path) {
			t.Errorf("%q should be accepted", path)
		}
	}
	for _, path := range []string{"//evil.example", "https://evil.example", "", "evil"} {
		if isLocalPath(path) {
			t.Errorf("%q must be rejected as an off-site redirect", path)
		}
	}
}
