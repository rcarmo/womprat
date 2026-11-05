#include "textflag.h"

// Native ABI entry: X0=method address, X1=receiver, X2=double bits. Tail-call
// put_ZoomFactor(X0=receiver, D0=zoom), preserving SyscallN's return address.
TEXT zoomTrampoline<>(SB),NOSPLIT|NOFRAME,$0
	MOVD R0, R9
	MOVD R1, R0
	FMOVD R2, F0
	JMP (R9)

TEXT ·zoomTrampolineAddress(SB),NOSPLIT|NOFRAME,$0-8
	MOVD $zoomTrampoline<>(SB), R0
	MOVD R0, ret+0(FP)
	RET
