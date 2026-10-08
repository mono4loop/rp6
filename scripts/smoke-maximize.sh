#!/usr/bin/env bash
# Live maximize check on a real Wayland display (make smoke-maximize). Builds
# rp6 with autopilot, runs docs/autopilot/maximize.txt (F9 maximizes, F9
# restores) and checks the RP6_DIAG log: the compositor really sized the
# maximize, rp6 switched to the console, and restoring brought back the window
# variant at the windowed size. It can't press the title bar itself (libdecor
# draws it), so the double-click stays a check by hand.
set -euo pipefail
cd "$(dirname "$0")/.."

bin=build/rp6-smoke
log=build/smoke-maximize.log
mkdir -p build
go build -tags "wayland migrated_fynedo autopilot" -o "$bin" ./cmd/rp6

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
if ! XDG_CONFIG_HOME="$tmp/config" XDG_DATA_HOME="$tmp/data" RP6_DIAG=1 \
	FADE_AUTOPILOT=docs/autopilot/maximize.txt timeout 60 "$bin" >"$log" 2>&1; then
	echo "smoke-maximize: FAIL - the script didn't reach quit (see $log)"
	exit 1
fi

fail() {
	echo "smoke-maximize: FAIL - $1 (see $log)"
	exit 1
}
grep -q 'layout variant -> console (maximized=true' "$log" || fail "maximizing didn't switch to the console"
grep -q 'layout variant -> window (maximized=false' "$log" || fail "restoring didn't switch back to the window"
# Widths of the main window over the run: the maximize must have widened it past
# the 850 design width (a maximize the compositor can't size leaves it as is),
# and the restore must have brought it back.
widths=$(sed -n 's/.*main window resized -> \([0-9]*\)x[0-9]*/\1/p' "$log")
widest=$(printf '%s\n' "$widths" | sort -n | tail -1)
last=$(printf '%s\n' "$widths" | tail -1)
[ "${widest:-0}" -gt 860 ] || fail "the compositor didn't size the maximize (widest canvas ${widest:-none})"
[ "${last:-9999}" -le 860 ] || fail "restoring didn't return to the windowed size (last canvas ${last:-none})"
echo "smoke-maximize: PASS (maximized to ${widest} wide, restored to ${last})"
