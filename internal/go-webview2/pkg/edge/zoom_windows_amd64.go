package edge

// Windows amd64 SyscallN mirrors each argument into the corresponding XMM
// register, so the double second parameter is passed in XMM1.
func callPutZoomFactor(proc ComProc, receiver uintptr, bits uint64) uintptr {
	hr, _, _ := proc.Call(receiver, uintptr(bits))
	return hr
}
