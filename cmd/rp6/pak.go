//go:build !js && !android && !ios

package main

import (
	"flag"
	"fmt"
	"os"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/storage"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/mono4loop/rp6/internal/samplepak"
	"github.com/mono4loop/rp6/internal/store"
)

// maybeRunPakCLI intercepts the `rp6 pak …` subcommand family before the GUI
// starts, running an offline command (create/install/list) and exiting. It
// returns without doing anything when the first argument isn't "pak", so normal
// GUI launch proceeds. Desktop-only (the samples directory is a real path).
func maybeRunPakCLI() {
	if len(os.Args) < 2 || os.Args[1] != "pak" {
		return
	}
	args := os.Args[2:]
	var err error
	switch {
	case len(args) == 0:
		err = fmt.Errorf("usage: rp6 pak <create|install|list> …")
	default:
		switch args[0] {
		case "create":
			err = pakCreate(args[1:])
		case "install":
			err = pakInstall(args[1:])
		case "list":
			err = pakList(args[1:])
		default:
			err = fmt.Errorf("unknown pak command %q (want create|install|list)", args[0])
		}
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "rp6 pak:", err)
		os.Exit(1)
	}
	os.Exit(0)
}

// pakCreate builds a .rp6sp archive from a directory of pad samples.
func pakCreate(args []string) error {
	fs := flag.NewFlagSet("pak create", flag.ContinueOnError)
	out := fs.String("o", "", "output .rp6sp path (default <id>.rp6sp)")
	id := fs.String("id", "", "pak id (stable, filesystem-safe; e.g. acme.kit) [required]")
	name := fs.String("name", "", "human-readable pak name [required]")
	version := fs.String("version", "", "pak version (e.g. 1.0.0)")
	author := fs.String("author", "", "author")
	desc := fs.String("desc", "", "short description")
	license := fs.String("license", "", "license (e.g. CC0-1.0)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("usage: rp6 pak create [flags] <samples-dir>")
	}
	if *id == "" || *name == "" {
		return fmt.Errorf("-id and -name are required")
	}
	outPath := *out
	if outPath == "" {
		outPath = *id + samplepak.Ext
	}
	m := samplepak.Manifest{
		ID:          *id,
		Name:        *name,
		Version:     *version,
		Author:      *author,
		Description: *desc,
		License:     *license,
	}
	if err := samplepak.Create(fs.Arg(0), outPath, m); err != nil {
		return err
	}
	fmt.Printf("created %s\n", outPath)
	return nil
}

// pakInstall installs a .rp6sp into the rp6 samples directory.
func pakInstall(args []string) error {
	fs := flag.NewFlagSet("pak install", flag.ContinueOnError)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("usage: rp6 pak install <pak.rp6sp>")
	}
	dir, err := store.SamplesDir()
	if err != nil {
		return err
	}
	installed, m, err := samplepak.Install(fs.Arg(0), dir)
	if err != nil {
		return err
	}
	fmt.Printf("installed %q (%s) -> %s\n", m.Name, m.ID, installed)
	return nil
}

// pakList lists installed paks.
func pakList(args []string) error {
	fs := flag.NewFlagSet("pak list", flag.ContinueOnError)
	if err := fs.Parse(args); err != nil {
		return err
	}
	dir, err := store.SamplesDir()
	if err != nil {
		return err
	}
	list, err := samplepak.List(dir)
	if err != nil {
		return err
	}
	if len(list) == 0 {
		fmt.Printf("no sample paks installed in %s\n", dir)
		return nil
	}
	for _, in := range list {
		fmt.Printf("%-24s %s\n", in.Manifest.ID, in.Manifest.Name)
	}
	return nil
}

// installAndSelectPak installs the pak at path and points the emulator at it,
// so `rp6 -pak foo.rp6sp` launches straight into that kit. On failure it logs
// and leaves emuDir/useEmu untouched (launch falls back to the built-in kit).
func (u *ui) installAndSelectPak(path string) {
	dir, err := store.SamplesDir()
	if err != nil {
		fmt.Fprintln(os.Stderr, "rp6: -pak:", err)
		return
	}
	installed, m, err := samplepak.Install(path, dir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "rp6: -pak install failed:", err)
		return
	}
	fmt.Printf("rp6: installed pak %q -> %s\n", m.Name, installed)
	u.emuDir = installed
	u.useEmu = true
}

// emuSettingsExtra returns the desktop-only "Install pak…" control shown in the
// emulator settings dialog. onInstalled is invoked after a successful install
// (to close the settings dialog). Desktop-only; the stub returns nil.
func (u *ui) emuSettingsExtra(onInstalled func()) []fyne.CanvasObject {
	btn := widget.NewButtonWithIcon("Install sample pak (.rp6sp)…", theme.ContentAddIcon(), func() {
		fd := dialog.NewFileOpen(func(rc fyne.URIReadCloser, err error) {
			if err != nil || rc == nil {
				return
			}
			path := rc.URI().Path()
			_ = rc.Close()
			u.installPakFile(path)
			if onInstalled != nil {
				onInstalled()
			}
		}, u.win)
		fd.SetFilter(storage.NewExtensionFileFilter([]string{samplepak.Ext}))
		fd.Show()
	})
	return []fyne.CanvasObject{btn}
}

// installPakFile installs a .rp6sp at path and switches the emulator to it.
func (u *ui) installPakFile(path string) {
	dir, err := store.SamplesDir()
	if err != nil {
		u.setStatus("couldn't locate samples dir: " + err.Error())
		return
	}
	installed, m, err := samplepak.Install(path, dir)
	if err != nil {
		u.setStatus("pak install failed: " + err.Error())
		return
	}
	u.setStatus("installed pak: " + m.Name)
	u.setEmuSamples(installed)
}
