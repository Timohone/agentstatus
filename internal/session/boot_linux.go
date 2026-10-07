//go:build linux

package session

import (
	"os"
	"strconv"
	"strings"
	"time"
)

// BootTime liefert den Systemstart aus /proc/stat (btime); Zeitnull, wenn unbekannt.
func BootTime() time.Time {
	b, err := os.ReadFile("/proc/stat")
	if err != nil {
		return time.Time{}
	}
	for _, l := range strings.Split(string(b), "\n") {
		if f := strings.Fields(l); len(f) == 2 && f[0] == "btime" {
			if n, err := strconv.ParseInt(f[1], 10, 64); err == nil {
				return time.Unix(n, 0).UTC()
			}
		}
	}
	return time.Time{}
}
