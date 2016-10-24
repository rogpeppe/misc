package auth

import (
	errgo "gopkg.in/errgo.v1"
	macaroon "gopkg.in/macaroon.v2-unstable"
)

// DischargeRequiredError is returned when authorization has failed
// and a discharged macaroon might fix it.
type DischargeRequiredError struct {
	Message string
	// Authenticator holds whether the macaroon should be treated
	// as an authentication macaroon. Authentication macaroons generally
	// have a longer lifespan than authorization macaroons.
	//
	// Authorization macaroons are usually acquired for the duration of
	// a request only and will usually not be stored into persistent storage.
	Authenticator bool
	Macaroon      *macaroon.Macaroon
}

func (e *DischargeRequiredError) Error() string {
	return "macaroon discharge required: " + e.Message
}

func isDischargeRequiredError(err error) bool {
	_, ok := err.(*DischargeRequiredError)
	return ok
}

type verificationError struct {
	error
}

func isVerificationError(err error) bool {
	_, ok := err.(*verificationError)
	return ok
}

var (
	ErrNotFound            = errgo.New("not found")
	ErrCaveatResultUnknown = errgo.New("caveat result not known")
)
