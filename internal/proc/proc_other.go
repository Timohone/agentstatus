//go:build !darwin && !linux

package proc

func Parent(pid int) (int, string, bool) { return 0, "", false }
