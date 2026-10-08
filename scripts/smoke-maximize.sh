#!/usr/bin/env bash
# Live maximize check on a real Wayland display (make smoke-maximize). Builds
# rp6 with autopilot and runs docs/autopilot/maximize.txt, which maximizes and
# restores the window through the compositor and waits for the console layout
# to show and go. The RP6_DIAG log then confirms the compositor really sized the
# maximize (a window whose minimum doesn't fit is flagged maximized unsized)
# and that restoring returned to the windowed width. The title bar itself is
# libdecor's, so the double-click stays a check by hand.
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
widths=$(sed -n 's/.*main window resized -> \([0-9]*\)x[0-9]*/\1/p' "$log")
widest=$(printf '%s\n' "$widths" | sort -n | tail -1)
last=$(printf '%s\n' "$widths" | tail -1)
[ "${widest:-0}" -gt 860 ] || fail "the compositor didn't size the maximize (widest canvas ${widest:-none})"
[ "${last:-9999}" -le 860 ] || fail "restoring didn't return to the windowed size (last canvas ${last:-none})"
echo "smoke-maximize: PASS (maximized to ${widest} wide, restored to ${last})"
