package api

import (
	"errors"
	"io/fs"
	"strings"
	"syscall"
	"testing"
)

func TestStoreFailureNamesAFullDisk(t *testing.T) {
	// Zo komt het echt binnen: os.WriteFile verpakt ENOSPC in een PathError.
	full := &fs.PathError{Op: "write", Path: "/var/lib/nodestatus-agent/devices.json.tmp", Err: syscall.ENOSPC}
	if msg := storeFailure(full, "the new device"); !strings.Contains(msg, "disk is full") {
		t.Errorf("full disk not named: %q", msg)
	}
	if msg := storeFailure(errors.New("boom"), "the new device"); strings.Contains(msg, "disk is full") {
		t.Errorf("other error reported as a full disk: %q", msg)
	}
}
