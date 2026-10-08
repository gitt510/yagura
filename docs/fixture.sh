#!/usr/bin/env bash
# Build a synthetic ghq-style tree for the README demo, under the directory
# given as $1 (used as a throwaway $HOME). Each fake repo gets a local bare
# "origin", so the MAIN and branch columns show real numbers with no network.
set -euo pipefail

home=$1
root="$home/ghq/github.com/acme"
origins="$home/origins"
mkdir -p "$root" "$origins"

# commits need an identity and a default branch inside the throwaway home
cat >"$home/.gitconfig" <<'EOF'
[user]
	name = demo
	email = demo@example.com
[init]
	defaultBranch = main
EOF
export HOME="$home"
export GIT_CONFIG_GLOBAL="$home/.gitconfig"
export GIT_AUTHOR_DATE="2026-01-01T00:00:00" GIT_COMMITTER_DATE="2026-01-01T00:00:00"

# new_repo <name>: a repo on main with one pushed commit and origin/HEAD set
new_repo() {
	git init -q --bare "$origins/$1.git"
	git init -q "$root/$1"
	git -C "$root/$1" remote add origin "$origins/$1.git"
	echo "# $1" >"$root/$1/README.md"
	git -C "$root/$1" add -A
	git -C "$root/$1" commit -qm "chore: init"
	git -C "$root/$1" push -qu origin main
	git -C "$root/$1" remote set-head origin main
}

# commit <name> <file> <message>: one pushed commit touching <file>
commit() {
	echo "$3" >>"$root/$1/$2"
	git -C "$root/$1" add -A
	git -C "$root/$1" commit -qm "$3"
}

# api-server: clean and in sync
new_repo api-server

# billing-worker: main behind origin by 2 (committed, pushed, rewound), and a
# fix branch whose remote side was deleted after its merge (gone)
new_repo billing-worker
commit billing-worker main.go "feat: add retry queue"
commit billing-worker main.go "fix: cap retry backoff"
git -C "$root/billing-worker" push -q origin main
git -C "$root/billing-worker" reset -q --hard HEAD~2
git -C "$root/billing-worker" switch -qc fix/timeout
git -C "$root/billing-worker" push -qu origin fix/timeout
git -C "$root/billing-worker" push -q origin --delete fix/timeout
git -C "$root/billing-worker" switch -q main

# demo-cli: a feature branch ahead of its upstream, with a dirty tree
new_repo demo-cli
git -C "$root/demo-cli" switch -qc feat/parser
commit demo-cli parser.go "feat: tokenize input"
commit demo-cli parser.go "feat: parse subcommands"
git -C "$root/demo-cli" push -qu origin feat/parser
commit demo-cli parser.go "feat: parse flags"
echo "wip" >>"$root/demo-cli/parser.go"
echo "notes" >"$root/demo-cli/TODO.md"
# a branch pushed and then dropped locally: it lives only on origin
git -C "$root/demo-cli" branch docs/usage main
git -C "$root/demo-cli" push -q origin docs/usage
git -C "$root/demo-cli" branch -qD docs/usage

# docs-site: in sync, one uncommitted edit, and a branch never pushed
new_repo docs-site
echo "draft" >>"$root/docs-site/README.md"
git -C "$root/docs-site" branch draft/intro
