//go:build darwin

package main

/*
#include <stdlib.h>
char *codexswitch_instances(void);
char *codexswitch_procargs(int pid, int *length);
int codexswitch_terminate_instance(int pid, double started);
*/
import "C"
import (
	"encoding/json"
	"errors"
	"fmt"
	"unsafe"
)

func runningCodexInstances() ([]codexInstance, error) {
	raw := C.codexswitch_instances()
	if raw == nil {
		return nil, errors.New("無法辨識 Codex 工作階段")
	}
	defer C.free(unsafe.Pointer(raw))
	var list []codexInstance
	if err := json.Unmarshal([]byte(C.GoString(raw)), &list); err != nil {
		return nil, err
	}
	for i := range list {
		var n C.int
		buf := C.codexswitch_procargs(C.int(list[i].PID), &n)
		if buf == nil {
			return nil, fmt.Errorf("無法讀取 Codex 程序 %d 的環境，請手動關閉該 App 後再試", list[i].PID)
		}
		bytes := C.GoBytes(unsafe.Pointer(buf), n)
		C.free(unsafe.Pointer(buf))
		args, env, err := parseProcessArgs(bytes)
		if err != nil {
			return nil, err
		}
		list[i].Home, list[i].Data, err = processDirectories(args, env)
		if err != nil {
			return nil, fmt.Errorf("無法確認 Codex 程序 %d 的目錄：%w", list[i].PID, err)
		}
	}
	return list, nil
}
func terminateCodexInstance(p codexInstance) error {
	if C.codexswitch_terminate_instance(C.int(p.PID), C.double(p.Started)) == 0 {
		return errors.New("對應 Codex App 拒絕關閉，或程序已變更；請手動關閉後再試")
	}
	return nil
}
