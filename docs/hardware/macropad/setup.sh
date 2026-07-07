#!/usr/bin/env bash
# setup.sh — one-shot prep for using an Adafruit MacroPad RP2040 with rp6.
#
# It copies the rp6 firmware (code.py, next to this script) onto the MacroPad's
# CIRCUITPY drive and installs the required CircuitPython libraries.
#
# Prerequisites:
#   * CircuitPython flashed on the MacroPad (see README.md if the CIRCUITPY
#     drive is missing) — the pad shows up as a USB drive named CIRCUITPY.
#   * `circup` for installing libraries:  pip install --user circup
#
# Usage:
#   docs/hardware/macropad/setup.sh [/path/to/CIRCUITPY]
#
# If the mount point is not given it is auto-detected.

set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
code_src="$script_dir/code.py"

die() { printf 'error: %s\n' "$*" >&2; exit 1; }
info() { printf '\033[1;33m==>\033[0m %s\n' "$*"; }

[ -f "$code_src" ] || die "firmware not found: $code_src"

# --- Locate the CIRCUITPY drive --------------------------------------------

find_circuitpy() {
    # Prefer a mount literally labelled CIRCUITPY (as CircuitPython names it).
    local candidates=(
        "/run/media/$USER/CIRCUITPY"
        "/media/$USER/CIRCUITPY"
        "/media/CIRCUITPY"
        "/Volumes/CIRCUITPY"   # macOS
    )
    local c
    for c in "${candidates[@]}"; do
        [ -d "$c" ] && { printf '%s\n' "$c"; return 0; }
    done
    # Fall back to scanning mounted filesystems for a CIRCUITPY label.
    local m
    m="$(mount 2>/dev/null | grep -i circuitpy | head -n1 | awk '{print $3}')" || true
    [ -n "$m" ] && { printf '%s\n' "$m"; return 0; }
    return 1
}

dest="${1:-}"
if [ -z "$dest" ]; then
    dest="$(find_circuitpy)" || die "CIRCUITPY drive not found — pass its path, or check the pad is plugged in and running CircuitPython (see README.md)."
fi
[ -d "$dest" ] || die "not a directory: $dest"
[ -w "$dest" ] || die "CIRCUITPY not writable: $dest"

info "MacroPad CIRCUITPY drive: $dest"

# --- Install libraries ------------------------------------------------------

if command -v circup >/dev/null 2>&1; then
    info "Installing CircuitPython libraries with circup (adafruit_macropad, adafruit_midi)…"
    circup --path "$dest" install adafruit_macropad adafruit_midi
else
    cat >&2 <<EOF
warning: 'circup' not found — skipping library install.
         Install it with:  pip install --user circup
         then re-run this script, or manually copy these from the
         Adafruit CircuitPython library bundle into $dest/lib/:
             adafruit_macropad, adafruit_midi,
             adafruit_display_text, adafruit_debouncer, adafruit_simple_text_display,
             neopixel, adafruit_hid, keypad (usually built in)
EOF
fi

# --- Copy the firmware ------------------------------------------------------

info "Copying firmware -> $dest/code.py"
cp "$code_src" "$dest/code.py"
sync

info "Done. The MacroPad will reload code.py automatically."
info "Plug both the MacroPad and the P-6 (or run rp6 -emu) and start rp6."
