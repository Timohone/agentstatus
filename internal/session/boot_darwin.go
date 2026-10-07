//go:build darwin

package session

import (
	"time"

	"golang.org/x/sys/unix"
)

// BootTime liefert den Systemstart; Zeitnull, wenn er sich nicht ermitteln laesst.
func BootTime() time.Time {
	tv, err := unix.SysctlTimeval("kern.boottime")
	if err != nil {
		return time.Time{}
	}
	return time.Unix(tv.Sec, int64(tv.Usec)*1000).UTC()
}
