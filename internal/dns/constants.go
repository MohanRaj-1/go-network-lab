package dns

const (
	TypeA    uint16 = 1
	TypeAAAA uint16 = 28
	ClassIN  uint16 = 1
)

const (
	FlagQR uint16 = 1 << 15
	FlagRD uint16 = 1 << 8
	FlagTC uint16 = 1 << 9
	FlagRA uint16 = 1 << 7
)

const (
	RCodeMask uint16 = 0x000F
)
