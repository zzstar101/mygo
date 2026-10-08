//go:build windows && arm64

#include "textflag.h"

// Windows ARM64: this in X0, method in X1, double bits in X2.
// Tail-call with the first floating argument in D0. No stack or nonvolatile
// register changes; SyscallN supplies the native call transition.
TEXT cookieExpiresThunk<>(SB), NOSPLIT|NOFRAME, $0
 FMOVD R2, F0
 JMP (R1)

TEXT ·cookieExpiresThunkAddress(SB), NOSPLIT, $0-8
 MOVD $cookieExpiresThunk<>(SB), R0
 MOVD R0, ret+0(FP)
 RET
