//go:build linux

package proc

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

func Parent(pid int) (int, string, bool) {
	b, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return 0, "", false
	}
	s := string(b)
	l, r := strings.IndexByte(s, '('), strings.LastIndexByte(s, ')')
	if l < 0 || r < l {
		return 0, "", false
	}
	f := strings.Fields(s[r+1:])
	if len(f) < 2 {
		return 0, "", false
	}
	ppid, err := strconv.Atoi(f[1])
	return ppid, s[l+1 : r], err == nil
}
