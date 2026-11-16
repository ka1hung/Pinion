package app

import (
	"errors"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// FileFilter is a dialog filter, e.g. {"Sessions", "*.ini;*.mxtsessions"}.
type FileFilter struct {
	Name    string `json:"name"`
	Pattern string `json:"pattern"`
}

func (c *Core) pickOpen(title string, dirs, multi bool, filters []FileFilter) ([]string, error) {
	if c.App == nil {
		return nil, errors.New("no UI")
	}
	d := c.App.Dialog.OpenFile().SetTitle(title).CanChooseFiles(!dirs).CanChooseDirectories(dirs).CanCreateDirectories(true).ShowHiddenFiles(true)
	for _, f := range filters {
		d.AddFilter(f.Name, f.Pattern)
	}
	if w := c.MainWindow(); w != nil {
		d.AttachToWindow(w)
	}
	if multi {
		return d.PromptForMultipleSelection()
	}
	p, err := d.PromptForSingleSelection()
	if err != nil || p == "" {
		return nil, err
	}
	return []string{p}, nil
}

func (c *Core) pickSave(title, filename string, filters []FileFilter) (string, error) {
	if c.App == nil {
		return "", errors.New("no UI")
	}
	d := c.App.Dialog.SaveFile().SetMessage(title).SetFilename(filename).CanCreateDirectories(true)
	for _, f := range filters {
		d.AddFilter(f.Name, f.Pattern)
	}
	if w := c.MainWindow(); w != nil {
		d.AttachToWindow(w)
	}
	return d.PromptForSingleSelection()
}

// PickFile opens a native open-file dialog; returns "" if cancelled.
func (c *Core) PickFile(title string, filters []FileFilter) (string, error) {
	r, err := c.pickOpen(title, false, false, filters)
	if err != nil || len(r) == 0 {
		return "", err
	}
	return r[0], nil
}

// PickFiles opens a multi-select open-file dialog.
func (c *Core) PickFiles(title string) ([]string, error) { return c.pickOpen(title, false, true, nil) }

// PickFolder opens a folder dialog.
func (c *Core) PickFolder(title string) (string, error) {
	r, err := c.pickOpen(title, true, false, nil)
	if err != nil || len(r) == 0 {
		return "", err
	}
	return r[0], nil
}

// PickSave opens a save dialog.
func (c *Core) PickSave(title, filename string, filters []FileFilter) (string, error) {
	return c.pickSave(title, filename, filters)
}

var _ = application.Window(nil)
