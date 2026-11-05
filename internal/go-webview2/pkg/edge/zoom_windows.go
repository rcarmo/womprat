//go:build windows

package edge

import (
	"fmt"
	"math"
	"runtime"
	"unsafe"
)

func (i *ICoreWebView2Controller) GetZoomFactor() (float64, error) {
	var zoom float64
	hr, _, _ := i.vtbl.GetZoomFactor.Call(uintptr(unsafe.Pointer(i)), uintptr(unsafe.Pointer(&zoom)))
	runtime.KeepAlive(i)
	if int32(hr) < 0 {
		return 0, fmt.Errorf("GetZoomFactor failed: HRESULT 0x%08x", uint32(hr))
	}
	if math.IsNaN(zoom) || math.IsInf(zoom, 0) || zoom <= 0 {
		return 0, fmt.Errorf("GetZoomFactor returned invalid zoom")
	}
	return zoom, nil
}

func (i *ICoreWebView2Controller) PutZoomFactor(zoom float64) error {
	if math.IsNaN(zoom) || math.IsInf(zoom, 0) || zoom <= 0 {
		return fmt.Errorf("invalid zoom factor")
	}
	hr := callPutZoomFactor(i.vtbl.PutZoomFactor, uintptr(unsafe.Pointer(i)), math.Float64bits(zoom))
	runtime.KeepAlive(i)
	if int32(hr) < 0 {
		return fmt.Errorf("PutZoomFactor failed: HRESULT 0x%08x", uint32(hr))
	}
	return nil
}

func (e *Chromium) GetZoomFactor() (float64, error) {
	if e.controller == nil {
		return 0, fmt.Errorf("browser controller unavailable")
	}
	return e.controller.GetZoomFactor()
}

func (e *Chromium) PutZoomFactor(zoom float64) error {
	if e.controller == nil {
		return fmt.Errorf("browser controller unavailable")
	}
	return e.controller.PutZoomFactor(zoom)
}
