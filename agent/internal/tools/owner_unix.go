package tools

import (
	"os"
	"os/user"
	"strconv"
	"sync"
	"syscall"
)

var (
	userMu    sync.Mutex
	userCache = map[uint32]string{}
)

func ownerName(fi os.FileInfo) string {
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return ""
	}
	userMu.Lock()
	defer userMu.Unlock()
	if n, ok := userCache[st.Uid]; ok {
		return n
	}
	name := strconv.FormatUint(uint64(st.Uid), 10)
	if u, err := user.LookupId(name); err == nil {
		name = u.Username
	}
	userCache[st.Uid] = name
	return name
}
