//go:build windows

package main

import "syscall"

func loadLibrary(path string) (uintptr, error) {
	handle, err := syscall.LoadLibrary(path)
	if err != nil {
		return 0, err
	}
	return uintptr(handle), nil
}

func unloadLibrary(handle uintptr) error {
	return syscall.FreeLibrary(syscall.Handle(handle))
}
