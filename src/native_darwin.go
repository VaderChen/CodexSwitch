//go:build darwin

package main

/*
#cgo darwin LDFLAGS: -framework Cocoa
void codexswitch_configure_window(void *ptr);
*/
import "C"
import "unsafe"

func configureNativeWindow(ptr unsafe.Pointer) { C.codexswitch_configure_window(ptr) }
