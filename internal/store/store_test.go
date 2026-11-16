package store

import (
	"os"
	"path/filepath"
	"testing"
)

// Settings saved by an older version must get defaults for new fields.
func TestSettingsDefaultsForMissingFields(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "settings.json"), []byte(`{"theme":"light"}`), 0o600)
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	st := s.Settings()
	if st.Theme != "light" || !st.SmartCopyPaste || st.TermRightClick != "menu" || !st.SFTPAutoSwitch || !st.SFTPFollowTerminal {
		t.Fatalf("defaults not applied: %+v", st)
	}
}
