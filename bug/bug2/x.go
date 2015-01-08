package bug2

type X struct {
	T      [32]byte
}

// Signature returns the macaroon's signature.
func (x *X) Get() []byte {
	t := x.T
	return t[:]
}
