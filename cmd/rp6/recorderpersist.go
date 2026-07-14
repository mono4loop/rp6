package main

import (
	"crypto/sha256"
	"fmt"
	"log"
	"path/filepath"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/dialog"
)

func (u *ui) recorderDir(profile string) (string, bool) {
	base, ok := recorderBaseDir()
	if !ok {
		return "", false
	}
	key := sha256.Sum256([]byte(profile))
	return filepath.Join(base, fmt.Sprintf("%x", key[:])), true
}

func (u *ui) loadRecorder() {
	u.recProfile = u.storeProfile()
	dir, ok := u.recorderDir(u.recProfile)
	if !ok {
		return
	}
	if err := u.rec.Load(dir); err != nil {
		log.Printf("rp6: recorder load: %v", err)
		u.setStatus("recorder load error: " + err.Error())
	}
	u.rec.SetTempo(u.bpm)
	if u.recRack != nil {
		u.recRack.syncAll()
	}
}

func (u *ui) autosaveRecorder() {
	if u.recProfile == "" {
		return
	}
	dir, ok := u.recorderDir(u.recProfile)
	if !ok {
		return
	}
	if err := u.rec.Save(dir); err != nil {
		log.Printf("rp6: recorder save: %v", err)
	}
}

func (u *ui) exportRecorderTrack(track int) {
	if !u.rec.HasClip(track) {
		u.setStatus(fmt.Sprintf("recorder track %d is empty", track+1))
		return
	}
	d := dialog.NewFileSave(func(w fyne.URIWriteCloser, err error) {
		if err != nil {
			u.setStatus("export error: " + err.Error())
			return
		}
		if w == nil {
			return
		}
		err = u.rec.ExportWAV(track, w)
		if closeErr := w.Close(); err == nil {
			err = closeErr
		}
		if err != nil {
			u.setStatus("export error: " + err.Error())
			return
		}
		u.setStatus(fmt.Sprintf("exported recorder track %d", track+1))
	}, u.win)
	d.SetFileName(fmt.Sprintf("rp6-track-%d.wav", track+1))
	d.Show()
}
