package tools

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRemoveOldGeekbenchDirs(t *testing.T) {
	root := t.TempDir()
	keep := filepath.Join(root, "Geekbench-6.7.1-Linux")
	for _, d := range []string{keep,
		filepath.Join(root, "Geekbench-6.7.0-Linux"),
		filepath.Join(root, "Geekbench-6.5.0-LinuxARMPreview"),
		filepath.Join(root, "iets-anders")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	removeOldGeekbenchDirs(root, keep)

	for _, want := range []string{keep, filepath.Join(root, "iets-anders")} {
		if _, err := os.Stat(want); err != nil {
			t.Errorf("had moeten blijven staan: %s", want)
		}
	}
	for _, gone := range []string{
		filepath.Join(root, "Geekbench-6.7.0-Linux"),
		filepath.Join(root, "Geekbench-6.5.0-LinuxARMPreview")} {
		if _, err := os.Stat(gone); err == nil {
			t.Errorf("had weg moeten zijn: %s", gone)
		}
	}
}
