//go:build !windows

package netpath

import "syscall"

func controlSocket(string, string, syscall.RawConn) error { return nil }
