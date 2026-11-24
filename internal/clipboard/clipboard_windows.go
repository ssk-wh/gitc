//go:build windows

package clipboard

import (
	"errors"
	"syscall"
	"time"
	"unsafe"
)

const (
	cfUnicodeText = 13
	gmemMoveable  = 0x0042
)

var (
	user32               = syscall.NewLazyDLL("user32.dll")
	kernel32             = syscall.NewLazyDLL("kernel32.dll")
	procOpenClipboard    = user32.NewProc("OpenClipboard")
	procCloseClipboard   = user32.NewProc("CloseClipboard")
	procEmptyClipboard   = user32.NewProc("EmptyClipboard")
	procSetClipboardData = user32.NewProc("SetClipboardData")
	procGlobalAlloc      = kernel32.NewProc("GlobalAlloc")
	procGlobalLock       = kernel32.NewProc("GlobalLock")
	procGlobalUnlock     = kernel32.NewProc("GlobalUnlock")
	procGlobalFree       = kernel32.NewProc("GlobalFree")
	procRtlMoveMemory    = syscall.NewLazyDLL("ntdll.dll").NewProc("RtlMoveMemory")
)

// Copy 直接调用 Win32 剪贴板 API 写入 Unicode 文本。
// 不再启动 PowerShell（实测启动需约 2 秒，导致成功提示迟迟不出现）。
func Copy(text string) error {
	utf16, err := syscall.UTF16FromString(text)
	if err != nil {
		return err
	}
	size := uintptr(len(utf16) * 2)
	h, _, _ := procGlobalAlloc.Call(gmemMoveable, size)
	if h == 0 {
		return errors.New("GlobalAlloc 失败")
	}
	ptr, _, _ := procGlobalLock.Call(h)
	if ptr == 0 {
		procGlobalFree.Call(h)
		return errors.New("GlobalLock 失败")
	}
	// 通过 RtlMoveMemory 写入，避免在 Go 里做 uintptr -> unsafe.Pointer 转换（go vet 会报错）
	procRtlMoveMemory.Call(ptr, uintptr(unsafe.Pointer(&utf16[0])), size)
	procGlobalUnlock.Call(h)

	// 剪贴板可能被其他进程短暂占用，重试几次。
	var ok uintptr
	for i := 0; i < 10; i++ {
		ok, _, _ = procOpenClipboard.Call(0)
		if ok != 0 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if ok == 0 {
		procGlobalFree.Call(h)
		return errors.New("剪贴板被其他程序占用")
	}
	defer procCloseClipboard.Call()

	if r, _, _ := procEmptyClipboard.Call(); r == 0 {
		procGlobalFree.Call(h)
		return errors.New("清空剪贴板失败")
	}
	// 成功后内存所有权归系统，不能再 GlobalFree。
	if r, _, _ := procSetClipboardData.Call(cfUnicodeText, h); r == 0 {
		procGlobalFree.Call(h)
		return errors.New("写入剪贴板失败")
	}
	return nil
}
