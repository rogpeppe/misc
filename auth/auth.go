// Package auth defines a structured way of authorizing access to a service.
//
// It relies on a third party (the identity service)
// to authenticate users and define group membership.
//
// When a user has authenticated themselves, they are allowed to obtain
// short-term capabilities that can be passed around so that third parties
// can act on their behalf.
//
// Users, groups, ACLs and entities
//
// A user represents some authenticated user. The nature of that
// authentication is not part of the scope of this package, but at the
// least each user has an id (the same each time a given user connects)
// and a domain that defines the name space within which the id lives.
///
// A group represents some collection of users. It is possible to find
// out whether a user is a member of a group, but not necessarily to
// find out all users that are part of the group. Groups live in the
// same name space as users - in general every user is a group with at
// least one member: itself.
//
// An ACL defines an access control list - a set of users and groups,
// any one of whom may access something. A user will be allowed to
// something protected by an ACL if the user is a member of any of the
// members of the ACL.
//
// An entity holds something in a service that is subject to
// authorization control. Every entity has a name that's defines it -
// every service will define its own set of entities. No service-defined
// entity should start with the prefixes "login" or "multi-" - these are
// reserved for internal use only.
//
// Operations, authorization and capabilities
//
// An operation defines some requested action on an entity. For example,
// a file system server might define an entity for every file in the
// server. In this case the entity name might be the file's path and the
// action might be one of "read", "write". The exact set of entities and
// actions is up to the caller, but should be kept stable over time
// because authorization tokens will contain these names.
//
// To authorize some request on behalf of a remote user, first find out
// what operations that request needs to perform. For example, if the
// user tries to delete a file, the entity might be the path to the
// file's directory and the action might be "write". It may often be
// possible to determine the operations required by a request without
// reference to anything external, when the request itself contains all
// the necessary information.
//
// The primary way of authorizing is by authentication. If a client can
// prove their identity, then Authorize can check whether they're a
// member of an ACL. If the client can't prove their identity, Authorize
// will return a macaroon that can be used to do so. This is known as an
// "authentication" macaroon and should not in general be passed to
// third parties because it can be used to perform any operation that
// the user is alowed.
//
// If a user is allowed to perform a set of operations, that user may
// also request a "capability" macaroon that can be used to perform
// those operations for a limited period of time. This differs from the
// authentication macaroon in that it authorizes the operations
// implicitly without the need for authentication, so can be passed to
// third parties to act on the user's behalf.
//
// Capabilities may be combined for authorization, so a request that
// involves several operations may be authorized by a set of
// capabilities that authorize all the operations between them, even
// though no single one is sufficient. Note that because a capability
// may be created for any set of operations that can be authorized, this
// means we can combine several capabilities into a single capability
// with the the capabilities of all.
//
// Third party caveats
//
// Sometimes just a set of operations is not sufficient to determine
// whether the user should be granted access to an entity. For example,
// we might need the user to verify something about the entity with some
// third party.
//
// This is why there is a caveats argument to Authorize and Capability.
// If a third party caveat is provided in it, it will have been checked
// before authorization succeeds.
package auth

import (
	"bytes"
	"sort"
	"strings"
	"time"

	"golang.org/x/net/context"

	errgo "gopkg.in/errgo.v1"
	"gopkg.in/macaroon-bakery.v2-unstable/bakery"
	"gopkg.in/macaroon-bakery.v2-unstable/bakery/checkers"
	macaroon "gopkg.in/macaroon.v2-unstable"
)

const (
	// Everyone is the name of the group that contains every user.
	Everyone = "everyone"

	DefaultCapabilityLifetime = 5 * time.Minute

	DefaultAuthenticationLifetime = 7 * 24 * time.Hour
)

// TODO what about long-lived authorization macaroons?

// TODO some kinds of operations may be awkward to express within this
// framework. For example, consider a "RemoveAll" operation that
// requires write access to all entities in a collection. If an entity
// is added between authorization and the action taking place, the
// authorization may become invalid. But in this case it's probably best
// not to create authorization for all the entities at once but to
// authorize for an entity that (for example) represents all the
// entities at a given point in time. If the entities change between
// authorization and action, we could just return "permission denied",
// or delete all entities older than that time or something.
//
// Concrete examples of this kind of thing would be useful.

// TODO what's the standard way of asking for authenticated access? LoginOp?

// Op holds an entity and action to be authorized on that entity.
type Op struct {
	// Action holds the action to perform on the entity, such as "read"
	// or "delete". It is up to the service using a checker to define
	// a set of operations and keep them consistent over time.
	Action string

	// Entity holds the name of the entity to be authorized.
	// Entity names should not contain spaces and should
	// not start with the prefix "login" or "multi-" (conventionally,
	// entity names will be prefixed with the entity type followed
	// by a hyphen.
	Entity string
}

// ACL holds a set of users and groups that are authorized to perform
// some action on an entity. The action is permitted if the
// authenticated user is a member of any of the named users or groups in
// the slice.
//
// Note that if the ACL is empty, nothing is permitted.
type ACL []string

// IsPublic reports whether the given ACL allows anyone access.
func (acl ACL) IsPublic() bool {
	for _, u := range acl {
		if u == Everyone {
			return true
		}
	}
	return false
}

// Params holds parameters for NewChecker.
type Params struct {
	// GetACLs returns the ACL that applies to each of the given
	// operations. It should return a slice with each element
	// holding the ACL for the corresponding operation in the
	// argument slice.
	//
	// If an entity cannot be found or the action is not recognised,
	// GetACLs should return an empty ACL entry for that operation.
	GetACLs func(ctxt context.Context, ops []Op) ([]ACL, error)

	// CaveatChecker is used to check first party caveats. If there
	// are any caveats whose value is unknown at the time of
	// checking, the checker should return ErrCaveatResultUnknown
	// when asked to check them, and they will be made available
	// after authorization via the PendingConditions field.
	//
	// The client of this package should make sure that all pending
	// caveat conditions are actually fully checked before
	// performing the action.
	//
	// CaveatChecker may be nil if no additional caveats are recognized.
	CaveatChecker checkers.Checker

	// StoreForEntity returns the macaroon storage to be
	// used for root keys associated with the given entity name.
	//
	// If this is nil, a store created by bakery.NewMemStorage will be used.
	StoreForEntity func(entity string) bakery.Storage

	// MultiOpStore is used to persistently store the association of
	// multi-op entities with their associated operations and
	// caveats.
	//
	// This will only be used when either:
	// - Capability is called with multiple operations
	// or
	// - Authorize is called with multiple operations and additional caveats.
	//
	// TODO if this is nil, embed the operations directly in the caveat id.
	MultiOpStore MultiOpStore

	// Key holds the private key pair of the service. It is used to
	// decrypt user information found in third party authentication
	// declarations and to encrypt third party caveats.
	Key *bakery.KeyPair

	// Locator is used to find out information on third parties when
	// adding third party caveats.
	Locator bakery.ThirdPartyLocator

	// IdentityService is used for interactions with the external
	// identity service used for authentication.
	IdentityService IdentityService

	// CapabilityLifetime holds the maximum lifetime of any issued capability
	// macaroon will last for. If zero, this defaults to DefaultCapabilityLifetime.
	CapabilityLifetime time.Duration

	// AuthenticationLifetime holds the maximum lifetime of any issued authentication
	// macaroon. If zero, this defaults to DefaultAuthenticationLifetime.
	AuthenticationLifetime time.Duration

	// Location holds the location that will be used in the created macaroons.
	Location string
}

// IdentityService represents the interactions of the authenticator with a
// trusted third party identity service.
type IdentityService interface {
	// IdentityCaveat encodes an identity caveat addressed to the identity
	// service that requests the service to authenticate the user.
	IdentityCaveat(key *bakery.PublicKey) checkers.Caveat

	// DeclaredUser parses the identity declaration from the
	// given declaration using the given key pair for decryption.
	DeclaredUser(declared checkers.Declared, key *bakery.KeyPair) (User, error)
}

// MultiOpStore holds the persistent store for operation sets.
// These are generally stored for a relatively short period of time,
// but if many clients are doing bulk operations on many
// disparate sets of entities the set might become large,
// so it's a potential DoS vector.
//
// The store must be suitable for concurrent use.
type MultiOpStore interface {
	// PutMultiOp creates an entry in the store associated with the given
	// key. A subsequent Get of the same key should result in the
	// same set of entities. Multiple puts of the same key should be
	// idempotent. The value associated with a given key will always
	// be the same.
	//
	// The context is derived from the context provided to Authorize
	// or Capability.
	//
	// The key must persist at least until the given expiry time.
	PutMultiOp(ctxt context.Context, key string, ops []Op, expiry time.Time) error

	// GetMultiOp returns the set of operations for a given key.
	// If the key was not found, it should return an error with an
	// ErrNotFound cause.
	//
	// The context is derived from the context provided to Authorize or Capability.
	//
	// TODO Perhaps this should return an interface that
	// can be used to check membership rather than the
	// whole set of operations. Then an implementation
	// might be able to scale more easily to large sets of
	// operations, for example by using a bloom filter.
	GetMultiOp(ctxt context.Context, key string) ([]Op, error)
}

// Checker represents an authorization and authentication checker
// for a service. Each client request should use a new Checker. After
// a successful Authorize or Capability call, the user information
// is available from the checker
type Checker struct {
	p Params

	// TODO domain and UserURL
}

// NewChecker returns a new checker using the given parameters.
func NewChecker(p Params) *Checker {
	if p.MultiOpStore == nil {
		p.MultiOpStore = NewMemMultiOpStore()
	}
	if p.StoreForEntity == nil {
		store := bakery.NewMemStorage()
		p.StoreForEntity = func(string) bakery.Storage {
			return store
		}
	}
	if p.AuthenticationLifetime == 0 {
		p.AuthenticationLifetime = DefaultAuthenticationLifetime
	}
	if p.CapabilityLifetime == 0 {
		p.CapabilityLifetime = DefaultCapabilityLifetime
	}
	if p.CaveatChecker != nil {
		p.CaveatChecker = checkers.New(p.CaveatChecker, checkers.TimeBefore)
	} else {
		p.CaveatChecker = checkers.TimeBefore
	}

	return &Checker{
		p: p,
	}
}

// Capability tries to obtain a capability that will provide
// authorization to execute any of the given operations. The returned
// capability may be delegated to third parties.
//
// If it succeeds, the returned macaroon may be delegated to third
// parties - it doesn't require authentication by default.
func (c *Checker) Capability(ctxt context.Context, mss []macaroon.Slice, clientVersion bakery.Version, ops []Op, caveats []checkers.Caveat) (*macaroon.Macaroon, *AuthInfo, error) {
	a, err := c.newAuthorizer(ctxt, mss, clientVersion, ops, caveats)
	if err != nil {
		return nil, nil, errgo.Mask(err)
	}
	authInfo, err := a.authorize(mss)
	if err != nil {
		return nil, nil, errgo.Mask(err, isDischargeRequiredError) // TODO errgo.Is(ErrPermissionDenied)
	}

	// We've now checked that the user has permission to execute all
	// the required operations so we can create the equivalent authz
	// capability.
	m, err := a.newAuthzMacaroon(false)
	if err != nil {
		return nil, nil, errgo.Notef(err, "cannot mint capability")
	}
	return m, authInfo, nil
}

// Authorize checks that the given set of discharged macaroons can
// authorize all the provided operations.
//
// All the given caveats will be checked before authorization is given.
//
// If the operation cannot be authorized because the authenticated user
// does not have the capability, an error with an ErrPermissionDenied (TODO)
// cause is returned. If the operation cannot be authorized because
// there is no valid authentication macaroon, an DischargeRequiredError
// is returned.
func (c *Checker) Authorize(ctxt context.Context, mss []macaroon.Slice, clientVersion bakery.Version, ops []Op, caveats []checkers.Caveat) (*AuthInfo, error) {
	a, err := c.newAuthorizer(ctxt, mss, clientVersion, ops, caveats)
	if err != nil {
		return nil, errgo.Mask(err)
	}
	return a.authorize(mss)
}

// authorizer holds state for an Authorize call.
type authorizer struct {
	*Checker

	// clientVersion holds the version of the bakery supported by the client.
	clientVersion bakery.Version

	// context holds the context provided to the Authorize call.
	context context.Context

	// ops holds all the operations that require authorization.
	ops []Op

	// caveats holds all the caveats that must holds.
	caveats []checkers.Caveat

	// ok holds an element for each member of ops
	// when records whether the given operation has
	// been authorized.
	ok []bool

	// pendingConditions holds an entry for
	// any caveat condition that needs to
	// be verified later.
	pendingConditions map[string]bool

	// macaroons holds the macaroons that
	// have been successfully used for authorization.
	macaroons []macaroon.Slice
}

// newAuthorizer returns a new authorizer that holds the given parameters.
func (c *Checker) newAuthorizer(ctxt context.Context, mss []macaroon.Slice, clientVersion bakery.Version, ops []Op, caveats []checkers.Caveat) (*authorizer, error) {
	if len(ops) == 0 {
		return nil, errgo.New("no operations allowed")
	}
	return &authorizer{
		Checker:       c,
		context:       ctxt,
		ops:           ops,
		clientVersion: clientVersion,
		// Note: restrict the capacity of the caveats slice so we can freely append
		// to it without worrying about side-effects.
		caveats: caveats[0:len(caveats):len(caveats)],
		ok:      make([]bool, len(ops)),
	}, nil
}

// authorize checks all the requested operations and caveats are authorized
// by the provided macaroons.
func (a *authorizer) authorize(mss []macaroon.Slice) (*AuthInfo, error) {
	for _, ms := range mss {
		if err := a.checkAuthzMacaroon(ms); err != nil {
			return nil, errgo.Mask(err)
		}
	}
	var user User
	// Even though we may have authorized everything above, we still process any authentication
	// macaroons so that we can provide authenticated user information.
	for _, ms := range mss {
		user1, pending, err := a.checkAuthnMacaroon(ms)
		if err != nil {
			if _, ok := errgo.Cause(err).(*verificationError); !ok {
				return nil, errgo.Mask(err)
			}
			// TODO log verification error?
			continue
		}
		user = user1
		a.addAuthorizedMacaroon(ms, pending)
		break
	}
	if !allTrue(a.ok) {
		// There are still some operations that need to be authorized.
		if err := a.authorizeWhenAuthenticated(user); err != nil {
			return nil, errgo.Mask(err, isDischargeRequiredError)
		}
	}
	pending := make([]string, 0, len(a.pendingConditions))
	for cond := range a.pendingConditions {
		pending = append(pending, cond)
	}
	// Stay deterministic by sorting the pending conditions.
	sort.Strings(pending)
	return &AuthInfo{
		PendingConditions: pending,
		Macaroons:         a.macaroons,
		User:              user,
	}, nil
}

// checkAuthzMacaroon checks whether the given macaroon slice is an
// authz (non-login) macaroon that's authorized to perform any of the
// given operations. If it is, it sets the respective elements in a.ok to
// true and adds the macaroon and any pending caveats to a.
//
// It returns an error only if it has an error trying to access the
// persistent stores.
func (a *authorizer) checkAuthzMacaroon(ms macaroon.Slice) error {
	if len(ms) == 0 {
		return nil
	}
	entityName, storageId := splitMacaroonId(ms[0].Id())
	if entityName == LoginEntity {
		// It's an authentication macaroon - we deal with those below differently
		// because only one authentication macaroon can apply.
		return nil
	}

	store := a.p.StoreForEntity(entityName)
	rootKey, err := store.Get([]byte(storageId))
	if err != nil && errgo.Cause(err) != bakery.ErrNotFound {
		return errgo.Notef(err, "cannot get root key for %q", storageId)
	}
	conditions, err := verifyIgnoringCaveats(ms, rootKey)
	if err != nil {
		// TODO log verification error?
		return nil
	}
	// We now know that the presented macaroon is valid for the
	// entity or entities that it describes. Now check
	// whether that authorizes any of the requested operations.
	for i, op := range a.ops {
		if a.ok[i] {
			// Operation is already authorized - no need to check again.
			continue
		}
		// Check that the macaroon's caveats check out OK with
		// the operation's action.
		declared, pending, err := a.checkConditions(conditions, op.Action)
		if err != nil {
			// The macaroon doesn't allow this operation but it might authorize others.
			// TODO log error?
			continue
		}
		if entityName == op.Entity {
			// The macaroon authorizes any operation on the named entity.
			// TODO add authorizing user infered from declared.
			a.ok[i] = true
			a.addAuthorizedMacaroon(ms, pending)
			continue
		}
		if !strings.HasPrefix(entityName, MultiOpEntityPrefix) {
			// The macaroon authorizes some other entity
			// we're not interested in right now.
			continue
		}
		// TODO if there's no multi-op store, we could guess at a common case
		// by getting the multi-op key for a.ops and if the hash is identical
		// we know we've got the right result.
		storedOps, err := a.p.MultiOpStore.GetMultiOp(a.context, entityName)
		if err != nil && errgo.Cause(err) != ErrNotFound {
			return errgo.Mask(err)
		}
		for _, storedOp := range storedOps {
			if storedOp == op {
				_ = declared // TODO include declared authorizing user.
				// Yay! The multi-entity includes the operation we're after.
				a.addAuthorizedMacaroon(ms, pending)
				a.ok[i] = true
				break
			}
		}
	}
	return nil
}

var errNotAuthnMacaroon = &verificationError{errgo.New("macaroon is not for authentication")}

// checkAuthnMacaroon checks whether the given macaroon can be used for authentication
// and that it's potentially valid for all the operations in a.ops that haven't already
// been authorized.
func (a *authorizer) checkAuthnMacaroon(ms macaroon.Slice) (user User, pending []string, err error) {
	if len(ms) == 0 {
		return nil, nil, errNotAuthnMacaroon
	}
	entityName, storageId := splitMacaroonId(ms[0].Id())
	if entityName != LoginEntity {
		return nil, nil, errNotAuthnMacaroon
	}
	loginStore := a.p.StoreForEntity(entityName)
	rootKey, err := loginStore.Get([]byte(storageId))
	if err != nil && errgo.Cause(err) != bakery.ErrNotFound {
		return nil, nil, errgo.Notef(err, "cannot get root key for %q", storageId)
	}
	conditions, err := verifyIgnoringCaveats(ms, rootKey)
	if err != nil {
		// TODO log verification error?
		return nil, nil, &verificationError{err}
	}
	// Check that the macaroon is valid for all the required operations.
	declared, pending, err := a.checkConditions(conditions, actionsForOps(a.ops)...)
	if err != nil {
		// Unlike an authz macaroon, we can use only one authn macaroon
		// and it must be good for all operations, so if there's an error here,
		// we can't use it.
		return nil, nil, &verificationError{err}
	}
	// The macaroon is valid for all the requested operations,
	// so use it for authentication and ignore any others.
	//
	// Is it bad that we can use an authn macaroon that explicitly
	// denies a particular action but we can still use it for identity
	// if we have explicit authorization (perhaps delegated) for that
	// action? I don't *think* so...
	user, err = a.p.IdentityService.DeclaredUser(declared, a.p.Key)
	if err != nil {
		return nil, nil, &verificationError{errgo.Notef(err, "cannot decode user info")}
	}
	return user, pending, nil
}

func actionsForOps(ops []Op) []string {
	// TODO de-duplicate actions?
	actions := make([]string, len(ops))
	for i, op := range ops {
		actions[i] = op.Action
	}
	return actions
}

// authorizeWhenAuthenticated checks that the authenticated user held in
// info (if any) is authorized to perform all the given operations and that
// all the caveats in a.caveats.
//
// It does not check operations that have already been successfully authorized.
func (a *authorizer) authorizeWhenAuthenticated(user User) error {
	// Find all the operations that still need authorization and fetch ACL
	// information for them.
	needOps := make([]Op, 0, len(a.ops))
	for i, op := range a.ops {
		if !a.ok[i] {
			needOps = append(needOps, op)
		}
	}
	acls, err := a.p.GetACLs(a.context, needOps)
	if err != nil {
		return errgo.Notef(err, "cannot retrieve ACLs")
	}
	for _, acl := range acls {
		var ok bool
		if user == nil {
			// No authenticated user but we'll allow them to do the
			// operation if the ACL is public.
			ok = acl.IsPublic()
		} else {
			ok1, err := user.Allow(a.context, acl)
			if err != nil {
				return errgo.Notef(err, "cannot check permissions")
			}
			ok = ok1
		}
		if ok {
			continue
		}
		if user != nil {
			return errgo.New("permission denied")
		}
		// The client hasn't authenticated yet - provide a way for
		// them to do so.
		return a.newAuthnDischargeRequiredError()
	}
	// We now know that the user is a member of all required ACLs.
	if len(a.caveats) == 0 {
		// No caveats: all good to go.
		return nil
	}

	// There are additional caveats. If they are only first-party caveats, then
	// just check them now, otherwise we need to return a discharge-required
	// error to create an authz macaroon that specifically authorizes those
	// caveats.
	//
	// We do this because authn macaroons should last for a while. We don't
	// want to return an authn macaroon that has limited scope - that's what authz macaroons are for.
	for _, cav := range a.caveats {
		if cav.Location != "" {
			// There's a third party caveat - create a new authz macaroon to discharge it.
			return a.newAuthzDischargeRequiredError()
		}
	}
	// No third party caveat - check that the caveats hold for all the operations too.
	// In practice there will probably almost never be an operation checker in
	// the provided caveats, but it's worth being consistent.
	conditions := make([]string, len(a.caveats))
	for i, cav := range a.caveats {
		conditions[i] = cav.Condition
	}
	_, pending, err := a.checkConditions(conditions, actionsForOps(a.ops)...)
	if err != nil {
		return errgo.Mask(err)
	}
	a.addPending(pending)
	// All caveats are satisfied.
	return nil
}

// newAuthnDischargeRequiredError mints an authn macaroon and returns it as a discharge-required error.
func (a *authorizer) newAuthnDischargeRequiredError() error {
	m, err := a.newMacaroon(LoginEntity, []checkers.Caveat{
		a.p.IdentityService.IdentityCaveat(&a.p.Key.Public),
		checkers.TimeBeforeCaveat(time.Now().Add(a.p.AuthenticationLifetime)),
	})
	if err != nil {
		return errgo.Mask(err)
	}
	return &DischargeRequiredError{
		Macaroon:      m,
		Authenticator: true,
		Message:       "login required",
	}
}

// newAuthzDischargeRequiredError mints an authz macaroon and returns it
// as a discharge-required error. The macaroon will be good for any of
// the required operations and will include all the required caveats.
func (a *authorizer) newAuthzDischargeRequiredError() error {
	m, err := a.newAuthzMacaroon(true)
	if err != nil {
		return errgo.Mask(err)
	}
	return &DischargeRequiredError{
		Macaroon:      m,
		Authenticator: true,
		Message:       "further authorization required",
	}
}

// newAuthzMacaroon returns a new macaroon which authorizes all
// the requested operations and has the requested caveats.
//
// The needCaveats argument specifies whether the authorizer caveats
// must be added.
func (a *authorizer) newAuthzMacaroon(needCaveats bool) (*macaroon.Macaroon, error) {
	entity, actions, err := a.authzEntity()
	if err != nil {
		return nil, errgo.Mask(err)
	}
	var caveats []checkers.Caveat
	if needCaveats {
		caveats = a.caveats
	}
	caveats = append(caveats, checkers.TimeBeforeCaveat(time.Now().Add(a.p.CapabilityLifetime)))
	if len(actions) != 0 {
		caveats = append(caveats, checkers.AllowCaveat(actions...))
	}
	m, err := a.newMacaroon(entity, caveats)
	if err != nil {
		return nil, errgo.Mask(err)
	}
	return m, nil
}

// newMacaroon returns a new macaroon associated with the given entity
// that has the given caveats.
//
// It also adds any pending caveat conditions so the new macaroon is
// still subject to them. It's OK to drop other first party caveats
// because they've checked out as valid.
//
// TODO It's not actually valid to drop other first party
// caveats - for example, currently this can be used to extend
// lifetime indefinitely. We could add all first party caveats from
// all the macaroons used for authorization, removing
// "allow" and "deny" caveats and being intelligent about
// "time-before" caveats.
func (a *authorizer) newMacaroon(entity string, caveats []checkers.Caveat) (*macaroon.Macaroon, error) {
	store := a.p.StoreForEntity(entity)
	rootKey, storageId, err := store.RootKey()
	if err != nil {
		return nil, errgo.Notef(err, "cannot get root key")
	}
	id := storageId
	if entityBytes := []byte(entity); !bytes.Equal(storageId, entityBytes) {
		id = make([]byte, 0, len(entity)+1+len(storageId))
		id = append(id, entityBytes...)
		id = append(id, ' ')
		id = append(id, storageId...)
	}
	// TODO add a version and a UUID to the macaroon id.
	m, err := macaroon.New(rootKey, id, a.p.Location, bakery.MacaroonVersion(a.clientVersion))
	if err != nil {
		return nil, errgo.Mask(err)
	}
	// Add any unchecked caveat conditions to the macaroon so that
	// even though we're not checking them here, they will be
	// checked eventually.
	// TODO sort conditions before adding so we get deterministic results?
	for cond := range a.pendingConditions {
		if err := m.AddFirstPartyCaveat(cond); err != nil {
			return nil, errgo.Notef(err, "cannot add first party caveat")
		}
	}
	for _, cav := range caveats {
		if err := bakery.AddCaveat(a.p.Key, a.p.Locator, m, cav); err != nil {
			return nil, errgo.Notef(err, "cannot add caveat")
		}
	}
	return m, nil
}

// authzEntity returns the name of the entity which will authorize all
// the required operations, and the set of actions that should be
// allowed on that entity. If allowActions is empty, all actions should
// be allowed.
func (a *authorizer) authzEntity() (entity string, allowActions []string, err error) {
	entity = a.ops[0].Entity
	for _, op := range a.ops[1:] {
		if op.Entity != entity {
			entity = ""
			break
		}
	}
	if entity != "" {
		// There's only one entity involved. Target the macaroon to that
		// entity and allow only the operations specified.
		// TODO if the entity id or the number of operations is huge,
		// we should use a multi-op entity anyway.
		actions := make([]string, len(a.ops))
		for i, op := range a.ops {
			actions[i] = op.Action
		}
		return entity, actions, nil
	}
	// Operations on multiple identities. Create a multi-op key and use that.
	entity, ops := NewMultiOpEntity(a.ops)
	if err := a.p.MultiOpStore.PutMultiOp(a.context, entity, ops, time.Now().Add(a.p.CapabilityLifetime)); err != nil {
		return "", nil, errgo.Notef(err, "cannot save multi-op entity")
	}
	return entity, nil, nil
}

// checkConditions checks that all the given conditions hold. It returns
// any verified declarations and any conditions whose results are not
// certain yet and which should be added to c.pendingConditions if the
// macaroon containing those conditions is used in the final
// authorization decision.
func (c *Checker) checkConditions(conditions []string, actions ...string) (declared checkers.Declared, pending []string, err error) {
	declared = checkers.InferDeclaredFromConditions(conditions)
	checker := checkers.New(
		c.p.CaveatChecker,
		declared,
		checkers.OperationsChecker(actions),
	)
	for _, cond := range conditions {
		if err := checker.CheckFirstPartyCaveat(cond); err != nil {
			if errgo.Cause(err) == ErrCaveatResultUnknown {
				pending = append(pending, cond)
			} else {
				return nil, nil, errgo.Mask(err, errgo.Any)
			}
		}
	}
	return declared, pending, nil
}

// addAuthorizedMacaroon should be called when a macaroon has successfully
// been used to contribute to the authorization. The conditions in pending
// should hold any conditiond whose result is not known.
func (a *authorizer) addAuthorizedMacaroon(ms macaroon.Slice, pending []string) {
	a.macaroons = append(a.macaroons, ms)
	a.addPending(pending)
}

// addPending adds the given conditions to a.pendingConditions.
func (a *authorizer) addPending(conditions []string) {
	if len(a.pendingConditions) == 0 {
		return
	}
	if a.pendingConditions == nil {
		a.pendingConditions = make(map[string]bool)
	}
	for _, cond := range conditions {
		a.pendingConditions[cond] = true
	}
}

func splitMacaroonId(id []byte) (entityName, storageId string) {
	entityName = string(id)
	storageId = entityName
	if space := bytes.IndexByte(id, ' '); space != -1 {
		entityName, storageId = entityName[0:space], entityName[space+1:]
	}
	return entityName, storageId
}

// verifyIgnoringCaveats verifies the given macaroon and its discharges without
// checking any caveats. It returns all the caveats that should
// have been checked.
func verifyIgnoringCaveats(ms macaroon.Slice, rootKey []byte) ([]string, error) {
	var caveats []string
	if len(ms) == 0 {
		return nil, errgo.New("no macaroons in slice")
	}
	if err := ms[0].Verify(rootKey, func(caveat string) error {
		caveats = append(caveats, caveat)
		return nil
	}, ms[1:]); err != nil {
		return nil, errgo.Mask(err)
	}
	return caveats, nil
}

// allTrue reports whether all elements of b are true.
func allTrue(b []bool) bool {
	for _, ok := range b {
		if !ok {
			return false
		}
	}
	return true
}
