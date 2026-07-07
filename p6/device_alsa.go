//go:build !js && !android

package p6

import (
	"bufio"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
)

// classifyOpenErr maps a raw open(2) error for path to a friendly sentinel
// (ErrBusy/ErrPermission), preserving the original via %w, or wraps it plainly.
func classifyOpenErr(path string, err error) error {
	switch {
	case errors.Is(err, syscall.EBUSY):
		return fmt.Errorf("%w: %s: %w", ErrBusy, path, err)
	case errors.Is(err, fs.ErrPermission):
		return fmt.Errorf("%w: %s: %w", ErrPermission, path, err)
	default:
		return fmt.Errorf("p6: opening MIDI device %s: %w", path, err)
	}
}

// Discover locates the ALSA raw MIDI device node for a connected P-6 by
// scanning /proc/asound/cards. It returns a path such as /dev/snd/midiC3D0.
func Discover() (string, error) {
	f, err := os.Open("/proc/asound/cards")
	if err != nil {
		return "", fmt.Errorf("p6: reading /proc/asound/cards: %w", err)
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		// A card header line looks like:
		//   " 3 [P6             ]: USB-Audio - P-6"
		// followed by an indented continuation line. We only care about the
		// header line, which starts with the card index.
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		idx, err := strconv.Atoi(fields[0])
		if err != nil {
			continue // continuation / non-header line
		}
		if !isP6Line(line) {
			continue
		}
		node, err := rawmidiNode(idx)
		if err != nil {
			return "", err
		}
		return node, nil
	}
	if err := sc.Err(); err != nil {
		return "", fmt.Errorf("p6: scanning /proc/asound/cards: %w", err)
	}
	return "", ErrNotFound
}

func isP6Line(line string) bool {
	l := strings.ToLower(line)
	return strings.Contains(l, "p-6") || strings.Contains(l, "[p6")
}

func rawmidiNode(card int) (string, error) {
	matches, err := filepath.Glob(fmt.Sprintf("/dev/snd/midiC%dD*", card))
	if err != nil {
		return "", fmt.Errorf("p6: globbing raw MIDI nodes: %w", err)
	}
	if len(matches) == 0 {
		return "", fmt.Errorf("p6: found P-6 on card %d but no raw MIDI node /dev/snd/midiC%dD*", card, card)
	}
	sort.Strings(matches)
	return matches[0], nil
}

// Open discovers a connected P-6 and opens it using the default configuration.
func Open() (*Device, error) {
	path, err := Discover()
	if err != nil {
		return nil, err
	}
	return OpenPath(path, DefaultConfig())
}

// OpenPath opens the raw MIDI device at path with the given configuration. It
// tries read/write first (so incoming MIDI can be read via Listen) and falls
// back to write-only if the node can't be opened for reading.
//
// If the node can't be opened at all, it returns a classified error:
// errors.Is(err, ErrBusy) when another program holds the exclusive port, or
// errors.Is(err, ErrPermission) for a permissions problem.
func OpenPath(path string, cfg Config) (*Device, error) {
	if f, err := os.OpenFile(path, os.O_RDWR, 0); err == nil {
		d := New(f, cfg)
		d.c = f
		d.r = f
		d.path = path
		return d, nil
	}
	f, err := os.OpenFile(path, os.O_WRONLY, 0)
	if err != nil {
		return nil, classifyOpenErr(path, err)
	}
	d := New(f, cfg)
	d.c = f
	d.path = path
	return d, nil
}
