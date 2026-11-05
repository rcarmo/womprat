package edge

// Windows ARM64 uses D0 for the first floating-point parameter, independently
// of the COM receiver in X0. SyscallN only loads integer registers. Invoke a
// tiny native trampoline through it so the Go runtime owns the foreign call.
func callPutZoomFactor(proc ComProc, receiver uintptr, bits uint64) uintptr {
	hr, _, _ := ComProc(zoomTrampolineAddress()).Call(uintptr(proc), receiver, uintptr(bits))
	return hr
}

func zoomTrampolineAddress() uintptr
