// Package auth defines a structured way of authorizing access to a
// service.
//
// It relies on a third party (the identity service) to authenticate
// users and define group membership.
//
// It uses macaroons as authorization tokens but it is not itself responsible for
// creating the macaroons - how and when to do that is considered
// a higher level thing.
//
// Identity and entities
//
// An Identity represents some user (or agent) authenticated by a third party.
//
// TODO
//
// Operations and authorization and capabilities
//
// An operation defines some requested action on an entity. For example,
// if file system server defines an entity for every file in the
// server, an operation to read a file might look like:
//
//     Op{
//		Entity: "/foo",
//		Action: "write",
//	}
//
// The exact set of entities and actions is up to the caller, but should
// be kept stable over time because authorization tokens will contain
// these names.
//
// To authorize some request on behalf of a remote user, first find out
// what operations that request needs to perform. For example, if the
// user tries to delete a file, the entity might be the path to the
// file's directory and the action might be "write". It may often be
// possible to determine the operations required by a request without
// reference to anything external, when the request itself contains all
// the necessary information.
//
// TODO update this.
//
// Third party caveats
//
// TODO.
package auth

import (
	"sort"
	"sync"
	"time"

	"github.com/juju/loggo"
	"golang.org/x/net/context"
	errgo "gopkg.in/errgo.v1"
	macaroon "gopkg.in/macaroon.v2-unstable"

	"gopkg.in/macaroon-bakery.v2-unstable/bakery"
	"gopkg.in/macaroon-bakery.v2-unstable/bakery/checkers"
)

var logger = loggo.GetLogger("bakery.auth")

// TODO think about a consistent approach to error reporting for macaroons.

// TODO should we really pass in explicit expiry times on each call to Allow?

var LoginOp = Op{
	Entity: "login",
	Action: "login",
}

var ErrPermissionDenied = errgo.New("permission denied")

type ServiceParams struct {
	// CaveatChecker is used to check first party caveats when authorizing.
	CaveatChecker checkers.Checker

	// UserChecker is used to check whether an authenticated user is
	// allowed to perform operations.
	//
	// The identity parameter passed to UserChecker.Allow will
	// always have been obtained from a call to
	// IdentityService.DeclaredIdentity.
	UserChecker UserChecker

	// IdentityService is used for interactions with the external
	// identity service used for authentication.
	IdentityClient IdentityService

	// MacaroonStore is used to retrieve macaroon root keys
	// and other associated information.
	MacaroonStore MacaroonStore
}

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

// MacaroonStore defines persistent storage for macaroon root keys.
type MacaroonStore interface {
	// MacaroonIdInfo returns information on the id of a macaroon.
	// TODO define some error type so we can distinguish storage errors
	// from bad ids and macaroon-not-found errors.
	// TODO this method isn't in a position to verify the macaroon
	// because it only has the id, which means that the information
	// in the id isn't verified before being acted on (for example to
	// get associated operatons from a persistent store).
	// Perhaps this method would be better as:
	// 	MacaroonInfo(ctxt context.Context, m *macaroon.Macaroon) ([]Op, []string, error)
	// where the method both verifies the macaroon and returns its caveat conditions
	// and the associared operations.
	MacaroonIdInfo(ctxt context.Context, id []byte) (rootKey []byte, ops []Op, err error)
}

// IdentityService represents the interactions of the authenticator with a
// trusted third party identity service.
type IdentityService interface {
	// IdentityCaveat encodes identity caveats addressed to the identity
	// service that request the service to authenticate the user.
	IdentityCaveats() []checkers.Caveat

	// DeclaredIdentity parses the identity declaration from the given
	// declared attributes.
	DeclaredIdentity(declared map[string]string) (Identity, error)
}

// AuthInfo information about an authorization decision.
type AuthInfo struct {
	// Identity holds information on the authenticated user as returned
	// from IdentityService.DeclaredUser. It may be nil after a
	// successful authorization if LoginOp access was not required.
	Identity Identity

	// Macaroons holds all the macaroons that were used for the
	// authorization. Macaroons that were invalid or unnecessary are
	// not included.
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

// Service represents an authorization service. It defines the identity
// service that's used as the root of trust, and persistent storage for
// macaroon root keys.
type Service struct {
	p             ServiceParams
	caveatChecker bakery.FirstPartyCaveatChecker
}

func NewService(p ServiceParams) *Service {
	checker := checkers.New(p.CaveatChecker)
	return &Service{
		p:             p,
		caveatChecker: checker,
	}
}

// UserChecker is used to check whether a given user is allowed
// to perform a set of operations.
type UserChecker interface {
	// Allow checks whether the given identity (which will be nil
	// when there is no authenticated user) is allowed to perform
	// the given operations. It should return an error only when
	// some underlying database operation has failed, not when the
	// user has been denied access.
	//
	// On success, each element of allowed holds whether the respective
	// element of ops has been allowed, and caveats holds any additional
	// third party caveats that apply.
	Allow(ctxt context.Context, id Identity, ops []Op) (allowed []bool, caveats []checkers.Caveat, err error)
}

// NewAuthorizer makes a new Authorizer instance using the
// given macaroons to inform authorization decisions.
func (s *Service) NewAuthorizer(mss []macaroon.Slice) *Authorizer {
	return &Authorizer{
		macaroons: mss,
		service:   s,
	}
}

type macaroonInfo struct {
	// index holds the index into the Request.Macaroons slice
	// of the macaroon that authorized the operation.
	index int

	// conditions holds the first party caveat conditions from the macaroons.
	conditions []string
}

// Authorizer authorizes operations with respect to a user's request.
type Authorizer struct {
	macaroons []macaroon.Slice
	// conditions holds the first party caveat conditions
	// that apply to each of the above macaroons.
	conditions [][]string
	service    *Service
	initOnce   sync.Once
	initError  error
	identity   Identity
	// authIndexes holds for each potentially authorized operation
	// the indexes of the macaroons that authorize it.
	authIndexes map[Op][]int
}

func (a *Authorizer) init(ctxt context.Context) error {
	a.initOnce.Do(func() {
		a.initError = a.initOnceFunc(ctxt)
	})
	return a.initError
}

func (a *Authorizer) initOnceFunc(ctxt context.Context) error {
	a.authIndexes = make(map[Op][]int)
	a.conditions = make([][]string, len(a.macaroons))
	for i, ms := range a.macaroons {
		if len(ms) == 0 {
			continue
		}
		rootKey, ops, err := a.service.p.MacaroonStore.MacaroonIdInfo(ctxt, ms[0].Id())
		if err != nil {
			logger.Infof("cannot get macaroon id info for %q\n", ms[0].Id())
			// TODO log error - if it's a storage error, return early here.
			continue
		}
		conditions, err := verifyIgnoringCaveats(ms, rootKey)
		if err != nil {
			logger.Infof("cannot verify %q: %v\n", ms[0].Id())
			// TODO log verification error
			continue
		}
		// It's a valid macaroon (in principle - we haven't checked first party caveats).
		if len(ops) == 1 && ops[0] == LoginOp {
			// It's an authn macaroon
			declared, err := a.checkConditions(ctxt, LoginOp, conditions)
			if err != nil {
				logger.Infof("caveat check failed, id %q: %v\n", ms[0].Id(), err)
				// TODO log error
				continue
			}
			if a.identity != nil {
				logger.Infof("duplicate authentication macaroon")
				// TODO log duplicate authn-macaroon error
				continue
			}
			identity, err := a.service.p.IdentityClient.DeclaredIdentity(declared)
			if err != nil {
				logger.Infof("cannot decode declared identity: %v", err)
				// TODO log user-decode error
				continue
			}
			a.identity = identity
		}
		a.conditions[i] = conditions
		for _, op := range ops {
			a.authIndexes[op] = append(a.authIndexes[op], i)
		}
	}
	logger.Infof("after init, identity: %#v, authIndexes %v", a.identity, a.authIndexes)
	return nil
}

// Allow checks that the authorizer's request is authorized to
// perform all the given operations. Note that Allow does not check
// first party caveats - if there is more than one macaroon that may
// authorize the request, it will choose the first one that does regardless
//
// If all the operations are allowed, an AuthInfo is returned holding
// details of the decision and any first party caveats that must be
// checked before actually executing any operation.
//
// If operations include LoginOp, the request must contain an
// authentication macaroon proving the client's identity. Once an
// authentication macaroon is chosen, it will be used for all other
// authorization requests.
//
// If an operation was not allowed, an error will be returned which may
// be *DischargeRequiredError holding the operations that remain to
// be authorized in order to allow authorization to
// proceed.
func (a *Authorizer) Allow(ctxt context.Context, ops []Op) (*AuthInfo, error) {
	authInfo, _, err := a.AllowAny(ctxt, ops)
	if err != nil {
		return nil, err
	}
	return authInfo, nil
}

type authInfo struct {
	identity Identity
	authed   []bool
}

// AllowAny is like Allow except that it will authorize as many of the
// operations as possible without requiring any to be authorized. If all
// the operations succeeded, the returned error and slice will be nil.
//
// If any the operations failed, the returned error will be the same
// that Allow would return and each element in the returned slice will
// hold whether its respective operation was allowed.
//
// If all the operations succeeded, the returned slice will be nil.
//
// The returned *AuthInfo will always be non-nil.
//
// The LoginOp operation is treated specially - it is always required if
// present in ops.
func (a *Authorizer) AllowAny(ctxt context.Context, ops []Op) (*AuthInfo, []bool, error) {
	authed, used, err := a.allowAny(ctxt, ops)
	return a.newAuthInfo(used), authed, err
}

func (a *Authorizer) newAuthInfo(used []bool) *AuthInfo {
	info := &AuthInfo{
		Identity:  a.identity,
		Macaroons: make([]macaroon.Slice, 0, len(a.macaroons)),
	}
	for i, isUsed := range used {
		if isUsed {
			info.Macaroons = append(info.Macaroons, a.macaroons[i])
		}
	}
	return info
}

// allowAny is the internal version of AllowAny. Instead of returning an
// authInfo struct, it returns a slice describing which operations have
// been successfully authorized and a slice describing which macaroons
// have been used in the authorization.
func (a *Authorizer) allowAny(ctxt context.Context, ops []Op) (authed, used []bool, err error) {
	if err := a.init(ctxt); err != nil {
		return nil, nil, errgo.Mask(err)
	}
	logger.Infof("after authorizer init, identity %#v", a.identity)
	used = make([]bool, len(a.macaroons))
	authed = make([]bool, len(ops))
	numAuthed := 0
	for i, op := range ops {
		if op == LoginOp && len(ops) > 1 {
			// LoginOp cannot be combined with other operations in the
			// same macaroon, so ignore it if it is.
			continue
		}
		for _, mindex := range a.authIndexes[op] {
			_, err := a.checkConditions(ctxt, op, a.conditions[mindex])
			if err != nil {
				logger.Infof("caveat check failed: %v", err)
				// log error?
				continue
			}
			authed[i] = true
			numAuthed++
			used[mindex] = true
			break
		}
	}
	if a.identity != nil {
		// We've authenticated as a user, so even if the operations didn't
		// specifically require it, we add the authn macaroon and its
		// conditions to the macaroons used and its con
		indexes := a.authIndexes[LoginOp]
		if len(indexes) == 0 {
			// Should never happen because init ensures it's there.
			panic("no macaroon info found for login op")
		}
		// Note: because we never issue a macaroon which combines LoginOp
		// with other operations, if the login op macaroon is used, we
		// know that it's already checked out successfully with LoginOp,
		// so no need to check again.
		used[indexes[0]] = true
	}
	if numAuthed == len(ops) {
		// All operations allowed.
		return nil, used, nil
	}
	// There are some unauthorized operations.
	need := make([]Op, 0, len(ops)-numAuthed)
	needIndex := make([]int, cap(need))
	for i, ok := range authed {
		if !ok {
			needIndex[len(need)] = i
			need = append(need, ops[i])
		}
	}
	logger.Infof("operations needed after authz macaroons: %#v", need)
	// Try to authorize the operations even even if we haven't got an authenticated user.
	oks, caveats, err := a.service.p.UserChecker.Allow(ctxt, a.identity, need)
	if err != nil {
		return authed, used, errgo.Notef(err, "cannot check permissions")
	}
	if len(oks) != len(need) {
		return authed, used, errgo.Newf("unexpected slice length returned from Allow (got %d; want %d)", len(oks), len(need))
	}

	stillNeed := make([]Op, 0, len(need))
	for i, ok := range oks {
		if ok {
			authed[needIndex[i]] = true
		} else {
			stillNeed = append(stillNeed, ops[needIndex[i]])
		}
	}
	if len(stillNeed) == 0 && len(caveats) == 0 {
		// No more ops need to be authenticated and no caveats to be discharged.
		return authed, used, nil
	}
	logger.Infof("operations still needed after auth check: %#v", stillNeed)
	if a.identity == nil {
		// User hasn't authenticated - ask them to do so.
		return authed, used, &DischargeRequiredError{
			Message: "authentication required",
			Ops:     []Op{LoginOp},
			Caveats: a.service.p.IdentityClient.IdentityCaveats(),
		}
	}
	if len(caveats) == 0 {
		return authed, used, ErrPermissionDenied
	}
	return authed, used, &DischargeRequiredError{
		Message: "some operations have extra caveats",
		Ops:     ops,
		Caveats: caveats,
	}
}

// AllowCapability checks that the user is allowed to perform all the
// given operations. If not, the error will be as returned from Allow.
//
// If AllowCapability succeeds, it returns a list of first party caveat
// conditions that must be applied to any macaroon granting capability
// to execute the operations.
//
// If ops contains LoginOp, the user must have been authenticated with a
// macaroon associated with the single operation LoginOp only.
func (a *Authorizer) AllowCapability(ctxt context.Context, ops []Op) ([]string, error) {
	nops := 0
	for _, op := range ops {
		if op != LoginOp {
			nops++
		}
	}
	if nops == 0 {
		return nil, errgo.Newf("no non-login operations required in capability")
	}
	_, used, err := a.allowAny(ctxt, ops)
	if err != nil {
		logger.Infof("allowAny returned used %v; err %v", used, err)
		return nil, errgo.Mask(err, isDischargeRequiredError)
	}
	var squasher caveatSquasher
	for i, isUsed := range used {
		if !isUsed {
			continue
		}
		for _, cond := range a.conditions[i] {
			squasher.add(cond)
		}
	}
	return squasher.final(), nil
}

// caveatSquasher rationalizes first party caveats created for a capability
// by:
//	- including only the earliest time-before caveat.
//	- excluding allow and deny caveats (operations are checked by
//	virtue of the operations associated with the macaroon).
//	- removing declared caveats.
//	- removing duplicates.
type caveatSquasher struct {
	expiry time.Time
	prev   string
	conds  []string
}

func (c *caveatSquasher) add(cond string) {
	// Don't add if already added.
	for _, added := range c.conds {
		if added == cond {
			return
		}
	}
	if c.add0(cond) {
		c.conds = append(c.conds, cond)
	}
}

func (c *caveatSquasher) final() []string {
	if !c.expiry.IsZero() {
		c.conds = append(c.conds, checkers.TimeBeforeCaveat(c.expiry).Condition)
	}
	if len(c.conds) == 0 {
		return nil
	}
	// Make deterministic and eliminate duplicates.
	sort.Strings(c.conds)
	prev := c.conds[0]
	j := 1
	for _, cond := range c.conds[1:] {
		if cond != prev {
			c.conds[j] = cond
			prev = cond
			j++
		}
	}
	return c.conds
}

func (c *caveatSquasher) add0(cond string) bool {
	cond, args, err := checkers.ParseCaveat(cond)
	if err != nil {
		// Be safe - if we can't parse the caveat, just leave it there.
		return true
	}
	switch cond {
	case checkers.CondTimeBefore:
		et, err := time.Parse(time.RFC3339Nano, args)
		if err != nil || et.IsZero() {
			// Again, if it doesn't seem valid, leave it alone.
			return true
		}
		if c.expiry.IsZero() || et.Before(c.expiry) {
			c.expiry = et
		}
		return false
	case checkers.CondAllow,
		checkers.CondDeny,
		checkers.CondDeclared:
		return false
	}
	return true
}

func (a *Authorizer) checkConditions(ctxt context.Context, op Op, conds []string) (map[string]string, error) {
	logger.Infof("checking conditions %q", conds)
	declared := checkers.InferDeclaredFromConditions(conds)
	ctxt = checkers.ContextWithOperations(ctxt, op.Action)
	ctxt = checkers.ContextWithDeclared(ctxt, declared)
	for _, cond := range conds {
		if err := a.service.caveatChecker.CheckFirstPartyCaveat(ctxt, cond); err != nil {
			return nil, errgo.Mask(err)
		}
	}
	return declared, nil
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
