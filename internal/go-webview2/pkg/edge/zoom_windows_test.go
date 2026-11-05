//go:build windows

package edge

import (
	"math"
	"runtime"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

func TestZoomControllerHRESULTAndValidation(t *testing.T) {
	calls := 0
	vtbl := &_ICoreWebView2ControllerVtbl{}
	controller := &ICoreWebView2Controller{vtbl: vtbl}
	vtbl.GetZoomFactor = NewComProc(func(_ uintptr, out *float64) uintptr {
		calls++
		*out = 1.25
		return 0
	})
	if zoom, err := controller.GetZoomFactor(); err != nil || zoom != 1.25 {
		t.Fatalf("zoom=%v err=%v", zoom, err)
	}
	vtbl.GetZoomFactor = NewComProc(func(_, _ uintptr) uintptr { return 0x80004005 })
	if _, err := controller.GetZoomFactor(); err == nil {
		t.Fatal("getter HRESULT ignored")
	}
	for _, invalid := range []float64{0, -1, math.NaN(), math.Inf(1)} {
		if err := controller.PutZoomFactor(invalid); err == nil {
			t.Fatalf("accepted invalid zoom %v", invalid)
		}
	}
	if calls != 1 {
		t.Fatal("invalid setter called COM")
	}
	vtbl.PutZoomFactor = NewComProc(func(_ uintptr) uintptr { return 0x80004005 })
	if err := controller.PutZoomFactor(1); err == nil {
		t.Fatal("setter HRESULT ignored")
	}
	if err := (&Chromium{}).PutZoomFactor(1); err == nil {
		t.Fatal("missing setter controller accepted")
	}
	if _, err := (&Chromium{}).GetZoomFactor(); err == nil {
		t.Fatal("missing controller accepted")
	}
}

func TestZoomSetterNativeFloatABI(t *testing.T) {
	// Native leaf procedure writes the floating-point argument into the
	// receiver, returns S_OK. NewCallback cannot test a float register ABI.
	var code []byte
	switch runtime.GOARCH {
	case "amd64":
		code = []byte{0xf2, 0x0f, 0x11, 0x09, 0x31, 0xc0, 0xc3} // movsd [rcx],xmm1; xor eax,eax; ret
	case "arm64":
		code = []byte{0x00, 0x00, 0x00, 0xfd, 0xe0, 0x03, 0x1f, 0x2a, 0xc0, 0x03, 0x5f, 0xd6} // str d0,[x0]; mov w0,wzr; ret
	default:
		t.Skip("unsupported native ABI")
	}
	address, err := windows.VirtualAlloc(0, uintptr(len(code)), windows.MEM_COMMIT|windows.MEM_RESERVE, windows.PAGE_READWRITE)
	if err != nil {
		t.Fatal(err)
	}
	defer windows.VirtualFree(address, 0, windows.MEM_RELEASE)
	copy(unsafe.Slice((*byte)(unsafe.Pointer(address)), len(code)), code)
	var old uint32
	if err := windows.VirtualProtect(address, uintptr(len(code)), windows.PAGE_EXECUTE_READ, &old); err != nil {
		t.Fatal(err)
	}
	// FlushInstructionCache is required before executing generated test code.
	flush := windows.NewLazySystemDLL("kernel32.dll").NewProc("FlushInstructionCache")
	if ok, _, err := flush.Call(^uintptr(0), address, uintptr(len(code))); ok == 0 {
		t.Fatal(err)
	}
	for _, value := range []float64{0.3, 1, 1.1, 3} {
		var got float64
		hr := callPutZoomFactor(ComProc(address), uintptr(unsafe.Pointer(&got)), math.Float64bits(value))
		runtime.KeepAlive(&got)
		if hr != 0 || math.Abs(got-value) > 1e-12 {
			t.Fatalf("native setter got=%v want=%v hr=%x", got, value, hr)
		}
	}
}
