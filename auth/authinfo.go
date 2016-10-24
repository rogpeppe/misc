package auth

import (
	"golang.org/x/net/context"

	"gopkg.in/macaroon-bakery.v2-unstable/bakery"
	macaroon "gopkg.in/macaroon.v2-unstable"
)

// AuthInfo holds information available after a successful authorization.
type AuthInfo struct {
	// PendingConditions holds any caveat conditions for which
	// the value was not known when Authorize or Capability
	// was called (that is, the checker returned ErrCaveatResultUnknown
	// for that condition). The caller must make sure to check
	// these conditions before actually performing the requested action.
	PendingConditions []string

	// User holds information on the authenticated
	// user. This may be nil after a successful authorization
	// if Login access was not required.
	User User

	// Macaroons holds all the macaroons that
	// were used for the authorization. Macaroons that were
	// invalid or unnecessary are not included.
	Macaroons []macaroon.Slice

	// TODO add information on user ids that have contributed
	// to the authorization:
	// After a successful call to Authorize or Capability,
	// AuthorizingUserIds returns the user ids that were used to
	// create the capability macaroons used to authorize the call.
	// Note that this is distinct from UserId, as there can only be
	// one authenticated user associated with the checker.
	// AuthorizingUserIds []string
}

// User holds user information declared in a first party caveat
// added when discharging a third party caveat.
type User interface {
	// Id returns the id of the user, which may be an
	// opaque blob with no human meaning.
	Id() string

	// Domain holds the domain of the user. This
	// will be empty if the user was authenticated
	// directly with the identity provider.
	Domain() string

	// PublicKey holds the public key that was
	// used to encrypt the user information.
	// It will be nil if the information was not encrypted.
	PublicKey() *bakery.PublicKey

	// Allow reports whether the user should be considered
	// to be part of any of the users or groups in the ACL.
	Allow(context.Context, ACL) (bool, error)
}
