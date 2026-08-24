//go:build unix

package tunbridge

import "golang.org/x/sys/unix"

// CloseFD закрывает дескриптор сетевого интерфейса.
func CloseFD(fd int) {
	if fd > 0 {
		_ = unix.Close(fd)
	}
}
