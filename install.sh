#!/usr/bin/env bash
# Installs habitat for the current user:
#
#   1. the `habitat` binary, via `go install`, linked onto your PATH
#   2. the `habitat` skill, into ~/.claude/skills/
#
# Usage:  ./install.sh              # both
#         ./install.sh --skill-only # skip the binary
#         ./install.sh --bin-only   # skip the skill
#         ./install.sh --link       # symlink the skill instead of copying, so
#                                   # edits in this repo take effect immediately
set -euo pipefail
cd "$(dirname "$0")"

CLAUDE_DIR="${CLAUDE_CONFIG_DIR:-$HOME/.claude}"
SKILL_DIR="$CLAUDE_DIR/skills/habitat"

want_bin=1
want_skill=1
link=0
for arg in "$@"; do
    case "$arg" in
        --skill-only) want_bin=0 ;;
        --bin-only) want_skill=0 ;;
        --link) link=1 ;;
        -h | --help)
            # The header comment is the help text; stop at the first line that
            # isn't one, so editing the header can't drift from a line number.
            awk 'NR>1 && !/^#/ {exit} NR>1 {sub(/^# ?/, ""); print}' "$0"
            exit 0
            ;;
        *)
            echo "unknown option: $arg" >&2
            exit 1
            ;;
    esac
done

say() { printf '  %s\n' "$1"; }

# go install's target directory is often not on PATH, and a binary you cannot
# type the name of is not installed as far as anyone is concerned. Symlink it
# into a directory that already is on PATH — a link rather than a copy, so a
# later `go install` is picked up without reinstalling.
put_on_path() {
    binary="$1"

    if [ "$(command -v habitat 2>/dev/null)" = "$binary" ]; then
        say "habitat is on your PATH"
        return 0
    fi

    for dir in "$HOME/.local/bin" "$HOME/bin" /usr/local/bin; do
        case ":$PATH:" in *":$dir:"*) ;; *) continue ;; esac
        [ -d "$dir" ] && [ -w "$dir" ] || continue

        # Never quietly replace something that isn't ours.
        if [ -e "$dir/habitat" ] && [ ! -L "$dir/habitat" ]; then
            say "WARNING: $dir/habitat exists and is not a symlink — leaving it alone"
            continue
        fi

        if [ -L "$dir/habitat" ] && [ "$(readlink "$dir/habitat")" = "$binary" ]; then
            say "already on your PATH: $dir/habitat"
            return 0
        fi

        ln -sf "$binary" "$dir/habitat"
        say "linked $dir/habitat -> $binary"
        return 0
    done

    say ""
    say "WARNING: no writable directory on your PATH to link into."
    say "Add the Go bin directory to your shell profile instead:"
    say "  export PATH=\"$(dirname "$binary"):\$PATH\""
}

# ── binary ───────────────────────────────────────────────────────────────────
if [ "$want_bin" = 1 ]; then
    echo "Installing the habitat binary"
    if ! command -v go >/dev/null; then
        echo "go is not installed. Install Go, or re-run with --skill-only." >&2
        exit 1
    fi
    go install ./cmd/habitat
    GOBIN="$(go env GOBIN)"
    [ -n "$GOBIN" ] || GOBIN="$(go env GOPATH)/bin"
    say "installed $GOBIN/habitat"
    put_on_path "$GOBIN/habitat"
fi

# ── skill ────────────────────────────────────────────────────────────────────
if [ "$want_skill" = 1 ]; then
    echo "Installing the habitat skill"
    mkdir -p "$(dirname "$SKILL_DIR")"

    # Replace whatever is there, including a stale symlink from an earlier
    # --link install pointing at a repo that has since moved.
    rm -rf "$SKILL_DIR"

    if [ "$link" = 1 ]; then
        ln -s "$PWD/skills/habitat" "$SKILL_DIR"
        say "linked $SKILL_DIR -> $PWD/skills/habitat"
    else
        cp -R skills/habitat "$SKILL_DIR"
        say "copied to $SKILL_DIR"
    fi
fi

echo
echo "Done. Start a new Claude Code session to pick up the skill."
if [ "$want_bin" = 1 ]; then
    echo "Try:  habitat --help"
fi
