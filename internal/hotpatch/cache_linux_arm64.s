#include "textflag.h"

// func clearCache(addr, n uint64)
//
// As compilers' __clear_cache does: clean the data cache to the point of
// unification, then invalidate the instruction cache, by cache line.
TEXT ·clearCache(SB), NOSPLIT, $0-16
	MOVD	addr+0(FP), R0
	MOVD	n+8(FP), R1
	ADD	R0, R1, R1
	MRS	CTR_EL0, R2
	// The smallest data cache line, in R3.
	UBFX	$16, R2, $4, R3
	MOVD	$4, R4
	LSL	R3, R4, R3
	// The smallest instruction cache line, in R4.
	AND	$15, R2, R5
	MOVD	$4, R4
	LSL	R5, R4, R4
	SUB	$1, R3, R6
	BIC	R6, R0, R5
data:
	CMP	R1, R5
	BHS	datadone
	WORD	$0xd50b7b25	// DC CVAU, R5
	ADD	R3, R5, R5
	B	data
datadone:
	DSB	$0xb	// ISH
	SUB	$1, R4, R6
	BIC	R6, R0, R5
inst:
	CMP	R1, R5
	BHS	instdone
	WORD	$0xd50b7525	// IC IVAU, R5
	ADD	R4, R5, R5
	B	inst
instdone:
	DSB	$0xb	// ISH
	ISB	$0xf
	RET
