package store

import (
	"errors"
	"syscall"
)

// IsDiskFull zegt of een fout uit Add/Replace komt doordat de schijf vol is.
// Dat is de enige opslagfout die in de praktijk voorkomt (gezien op een node
// waar een backupjob de schijf had volgeschreven), en de enige waar de
// gebruiker zelf iets aan kan doen — dus die verdient een eigen melding.
func IsDiskFull(err error) bool {
	return errors.Is(err, syscall.ENOSPC) || errors.Is(err, syscall.EDQUOT)
}

// FreeBytes geeft de ruimte die een gewone gebruiker in dir nog kan schrijven.
// Bavail en niet Bfree: de agent draait niet als root, dus de voor root
// gereserveerde blokken (standaard 5%) helpen hem niet.
func FreeBytes(dir string) (uint64, bool) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(dir, &st); err != nil {
		return 0, false
	}
	return uint64(st.Bavail) * uint64(st.Bsize), true
}
