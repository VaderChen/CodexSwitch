//go:build darwin

package main

/*
#cgo darwin LDFLAGS: -framework Cocoa
void codexswitch_configure_window(void *ptr);
int codexswitch_button_hints(void);
void codexswitch_save_button_hints(int enabled);
*/
import "C"
import "unsafe"

func configureNativeWindow(ptr unsafe.Pointer) { C.codexswitch_configure_window(ptr) }

func buttonHintsPreference() bool { return C.codexswitch_button_hints() != 0 }
func saveButtonHintsPreference(enabled bool) {
	var value C.int
	if enabled {
		value = 1
	}
	C.codexswitch_save_button_hints(value)
}
