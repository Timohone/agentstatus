//go:build !darwin && !linux

package session

import "time"

// BootTime: unbekannt, die Neustart-Regel greift dort nicht.
func BootTime() time.Time { return time.Time{} }
