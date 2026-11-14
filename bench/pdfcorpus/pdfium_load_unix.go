//go:build !windows && !(js && wasm)

package main

import "github.com/ebitengine/purego"

func loadLibrary(path string) (uintptr, error) {
	return purego.Dlopen(path, purego.RTLD_NOW|purego.RTLD_GLOBAL)
}

func unloadLibrary(handle uintptr) error {
	return purego.Dlclose(handle)
}
