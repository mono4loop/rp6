package midiin

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// FindRawMIDI scans /proc/asound/cards for a sound card whose header line
// matches (case-insensitively contains) any of nameSubstrings, and returns that
// card's first raw MIDI node (e.g. /dev/snd/midiC3D0). It is the discovery
// primitive shared by input-controller drivers — the input-side analogue of
// p6.Discover, kept here so drivers don't reimplement /proc parsing.
//
// It returns ok=false (never an error) when no matching card with a MIDI node
// is present, so Driver.Detect can forward the result directly.
func FindRawMIDI(nameSubstrings ...string) (path string, ok bool) {
	f, err := os.Open("/proc/asound/cards")
	if err != nil {
		return "", false
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		// Card header lines start with the card index, e.g.
		//   " 2 [MacroPad       ]: USB-Audio - MacroPad"
		// followed by an indented continuation line we skip.
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		idx, err := strconv.Atoi(fields[0])
		if err != nil {
			continue // continuation / non-header line
		}
		if !lineMatchesAny(line, nameSubstrings) {
			continue
		}
		if node, ok := rawmidiNode(idx); ok {
			return node, true
		}
	}
	return "", false
}

func lineMatchesAny(line string, subs []string) bool {
	l := strings.ToLower(line)
	for _, s := range subs {
		if s != "" && strings.Contains(l, strings.ToLower(s)) {
			return true
		}
	}
	return false
}

func rawmidiNode(card int) (string, bool) {
	matches, err := filepath.Glob(fmt.Sprintf("/dev/snd/midiC%dD*", card))
	if err != nil || len(matches) == 0 {
		return "", false
	}
	sort.Strings(matches)
	return matches[0], true
}
