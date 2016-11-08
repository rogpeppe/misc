package auth_test

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"time"

	"github.com/juju/httprequest"
	"github.com/juju/idmclient"
	"github.com/juju/idmclient/idmtest"
	"github.com/juju/loggo"
	jujutesting "github.com/juju/testing"
	"golang.org/x/net/context"
	gc "gopkg.in/check.v1"
	errgo "gopkg.in/errgo.v1"
	"gopkg.in/macaroon.v2-unstable"

	"github.com/rogpeppe/misc/auth"
	"gopkg.in/macaroon-bakery.v2-unstable/bakery"
	"gopkg.in/macaroon-bakery.v2-unstable/bakery/checkers"
	"gopkg.in/macaroon-bakery.v2-unstable/bakerytest"
	"gopkg.in/macaroon-bakery.v2-unstable/httpbakery"
)

var logger = loggo.GetLogger("bakery.auth_test")

const Everyone = "everyone"

var allCheckers = checkers.New(
	checkers.TimeBefore,
	checkers.Declared,
	checkers.OperationChecker,
)

// TODO move idmclient to latest bakery version so we can avoid
// double dependencies above.

type authSuite struct {
	jujutesting.LoggingSuite
}

var _ = gc.Suite(&authSuite{})

// AuthHTTPHandler represents an HTTP handler that can be queried
// for authorization information.
type AuthHTTPHandler interface {
	http.Handler
	// EndpointAuth returns the operations and caveats required by the
	// endpoint implied by the given request.
	// TODO return caveats too.
	EndpointAuth(req *http.Request) []auth.Op
}

func (*authSuite) TestAuthorizeWithoutHTTPBakery(c *gc.C) {
	h := testHandler{}
	s := newTestServers(h, ACLMap{
		"path-/": {
			"GET": {Everyone},
		},
	})
	defer s.Close()

	// Anyone should be able to access /, so try it without a bakery client.
	resp, err := http.Get(s.svc.URL + "/")
	c.Assert(err, gc.IsNil)
	h.assertSuccess(c, resp, "GET", "/")
}

func (*authSuite) TestEndpointWithAuthenticationRequired(c *gc.C) {
	h := testHandler{}
	s := newTestServers(h, ACLMap{
		"path-/bob": {
			"PUT": {"bob"},
		},
	})
	defer s.Close()

	client := s.idmSrv.Client("bob")
	req, err := http.NewRequest("PUT", s.svc.URL+"/bob", nil)
	c.Assert(err, gc.IsNil)
	resp, err := client.Do(req)
	c.Assert(err, gc.IsNil)
	h.assertSuccess(c, resp, "PUT", "/bob")
}

func (*authSuite) TestAuthorizeMultipleEntities(c *gc.C) {
	h := testHandler{}
	s := newTestServers(h, ACLMap{
		"path-/": {
			"GET": {Everyone},
		},
		"path-/bob": {
			"GET": {"bob"},
			"PUT": {"bob"},
		},
		"path-/alice": {
			"GET": {"alice", "bob"},
			"PUT": {"alice"},
		},
	})
	defer s.Close()

	client := s.idmSrv.Client("bob")
	req, err := http.NewRequest("GET", s.svc.URL+"/bob?e=/alice", nil)
	c.Assert(err, gc.IsNil)
	resp, err := client.Do(req)
	c.Assert(err, gc.IsNil)
	h.assertSuccess(c, resp, "GET", "/bob")
}

func (*authSuite) TestCapability(c *gc.C) {
	h := testHandler{}
	s := newTestServers(h, ACLMap{
		"path-/bob": {
			"GET": {"bob"},
		},
	})
	defer s.Close()

	ms := getCapability(c, s.idmSrv.Client("bob"), "GET", s.svc.URL+"/bob")

	// With this capability, we should be able to do a get without any other
	// authorization.
	data, err := json.Marshal(ms)
	c.Assert(err, gc.IsNil)
	value := base64.StdEncoding.EncodeToString(data)

	req, err := http.NewRequest("GET", s.svc.URL+"/bob", nil)
	c.Assert(err, gc.IsNil)
	req.Header.Set(httpbakery.MacaroonsHeader, value)

	resp, err := http.DefaultClient.Do(req)
	c.Assert(err, gc.IsNil)
	h.assertSuccess(c, resp, "GET", "/bob")
}

func (*authSuite) TestCapabilityMultipleEntities(c *gc.C) {
	h := testHandler{}
	s := newTestServers(h, ACLMap{
		"path-/bob": {
			"GET": {"bob"},
			"PUT": {"bob"},
		},
		"path-/alice": {
			"GET": {"alice", "bob"},
			"PUT": {"alice"},
		},
	})
	defer s.Close()

	url := s.svc.URL + "/bob?e=/alice"
	ms := getCapability(c, s.idmSrv.Client("bob"), "GET", url)

	// With this capability, we should be able to do a get without any other
	// authorization.
	resp := doWithCapabilities(c, http.DefaultClient, "GET", url, ms)
	h.assertSuccess(c, resp, "GET", "/bob")
}

func (*authSuite) TestMultipleCapabilities(c *gc.C) {
	h := testHandler{}
	s := newTestServers(h, ACLMap{
		"path-/bob": {
			"GET": {"bob"},
		},
		"path-/alice": {
			"GET": {"alice"},
		},
	})
	defer s.Close()

	bobCap := getCapability(c, s.idmSrv.Client("bob"), "GET", s.svc.URL+"/bob")
	aliceCap := getCapability(c, s.idmSrv.Client("alice"), "GET", s.svc.URL+"/alice")

	// We should be able to use both capabilities to act
	// on two endpoints at once.
	resp := doWithCapabilities(c, http.DefaultClient, "GET", s.svc.URL+"/bob?e=/alice", bobCap, aliceCap)
	h.assertSuccess(c, resp, "GET", "/bob")
}

func (*authSuite) TestCombineCapabilities(c *gc.C) {
	h := testHandler{}
	s := newTestServers(h, ACLMap{
		"path-/bob": {
			"GET": {"bob"},
		},
		"path-/alice": {
			"GET": {"alice"},
		},
	})
	defer s.Close()
	bobCap := getCapability(c, s.idmSrv.Client("bob"), "GET", s.svc.URL+"/bob")
	aliceCap := getCapability(c, s.idmSrv.Client("alice"), "GET", s.svc.URL+"/alice")

	// We should be able to combine both capabilities into a single one.
	bothCap := getCapability(c, httpbakery.NewClient(), "GET", s.svc.URL+"/bob?e=/alice", bobCap, aliceCap)

	c.Logf("bothCap id %q", bothCap[0].Id())

	// We should be able to use the new capability to act as both endpoints at once.
	resp := doWithCapabilities(c, http.DefaultClient, "GET", s.svc.URL+"/bob?e=/alice", bothCap)
	h.assertSuccess(c, resp, "GET", "/bob")

	// We should also be able to use it to act on one of the entities only.
	resp = doWithCapabilities(c, http.DefaultClient, "GET", s.svc.URL+"/alice", bothCap)
	h.assertSuccess(c, resp, "GET", "/alice")
}

func (*authSuite) TestAuthnWithAuthz(c *gc.C) {
	h := testHandler{}
	s := newTestServers(h, ACLMap{
		"path-/bob": {
			"GET": {"bob"},
		},
		"path-/alice": {
			"GET": {"alice"},
		},
	})
	defer s.Close()

	aliceCap := getCapability(c, s.idmSrv.Client("alice"), "GET", s.svc.URL+"/alice")

	resp := doWithCapabilities(c, s.idmSrv.Client("bob"), "GET", s.svc.URL+"/bob?e=/alice", aliceCap)
	h.assertSuccess(c, resp, "GET", "/bob")
}

func (*authSuite) TestAuthWithThirdPartyCaveats(c *gc.C) {
	checked := 0
	thirdParty := bakerytest.NewDischarger(nil, httpbakery.ThirdPartyCheckerFunc(
		func(req *http.Request, info *bakery.ThirdPartyCaveatInfo) ([]checkers.Caveat, error) {
			checked++
			c.Check(info.Condition, gc.Equals, "important caveat")
			return nil, nil
		},
	))
	defer thirdParty.Close()
	getACL := func(context.Context, auth.Op) (ACL, []checkers.Caveat, error) {
		return ACL{"bob"}, []checkers.Caveat{{
			Condition: "important caveat",
			Location:  thirdParty.Location(),
		}}, nil
	}

	h := testHandler{}
	s := newTestServers(h, ACLGetterFunc(getACL))
	defer s.Close()

	client := s.idmSrv.Client("bob")
	req, err := http.NewRequest("GET", s.svc.URL+"/hello", nil)
	c.Assert(err, gc.IsNil)
	resp, err := client.Do(req)
	c.Assert(err, gc.IsNil)
	h.assertSuccess(c, resp, "GET", "/hello")

	c.Assert(checked, gc.Equals, 1)
}

func (*authSuite) TestLoginOpIgnoredIfCombined(c *gc.C) {
	// TODO
}

func (*authSuite) TestLoginMacaroonWithFirstPartyCaveats(c *gc.C) {
	// TODO
}

func (*authSuite) TestAuthorizationMacaroonWithFirstPartyCaveats(c *gc.C) {
	// TODO
}

func (*authSuite) TestAllowCapabilityWithNoNonLoginOps(c *gc.C) {
	// TODO
}

func (*authSuite) TestUnusedMacaroons(c *gc.C) {
	// TODO
}

func (*authSuite) TestFirstPartyCaveatSquashing(c *gc.C) {
	// TODO
}

func (*authSuite) TestAuthorizeWithEmptyMacaroonSlice(c *gc.C) {
	// TODO
}

func (*authSuite) TestMacaroonWithCorruptedSignature(c *gc.C) {
	// TODO
}

func (*authSuite) TestAllowAny(c *gc.C) {
	// TODO
}

type testServers struct {
	idmSrv *idmtest.Server
	svc    *httptest.Server
}

func newTestServers(h AuthHTTPHandler, acls ACLGetter) *testServers {
	idmSrv := idmtest.NewServer()
	return &testServers{
		idmSrv: idmSrv,
		svc: newAuthHTTPService(h,
			idmClientShim{idmSrv.IDMClient("auth-user")},
			acls,
			allCheckers,
		),
	}
}

func (s *testServers) Close() {
	s.svc.Close()
	s.idmSrv.Close()
}

type idmClientShim struct {
	idmclient.IdentityClient
}

func (c idmClientShim) DeclaredIdentity(attrs map[string]string) (auth.Identity, error) {
	return c.IdentityClient.DeclaredIdentity(attrs)
}

type httpDoer interface {
	Do(*http.Request) (*http.Response, error)
}

func doWithCapabilities(c *gc.C, client httpDoer, method, url string, mss ...macaroon.Slice) *http.Response {
	req, err := http.NewRequest(method, url, nil)
	c.Assert(err, gc.IsNil)
	addCapabilities(req, mss)

	resp, err := client.Do(req)
	c.Assert(err, gc.IsNil)
	return resp
}

func getCapability(c *gc.C, client *httpbakery.Client, method, url string, mss ...macaroon.Slice) macaroon.Slice {
	req, err := http.NewRequest("AUTH", url, nil)
	c.Assert(err, gc.IsNil)
	req.Header.Set("AuthMethod", method)
	addCapabilities(req, mss)
	resp, err := client.Do(req)
	c.Assert(err, gc.IsNil)
	defer resp.Body.Close()
	data, err := ioutil.ReadAll(resp.Body)
	c.Assert(err, gc.IsNil)
	c.Assert(resp.StatusCode, gc.Equals, http.StatusOK, gc.Commentf("body: %q", data))
	var capResp capabilityResponse

	err = json.Unmarshal(data, &capResp)
	c.Assert(err, gc.IsNil, gc.Commentf("response body: %q", data))
	c.Assert(capResp.Macaroon, gc.NotNil)
	c.Logf("successfully got capability")

	ms, err := client.DischargeAll(capResp.Macaroon)
	c.Assert(err, gc.IsNil)
	return ms
}

func addCapabilities(req *http.Request, mss []macaroon.Slice) {
	for _, ms := range mss {
		data, err := json.Marshal(ms)
		if err != nil {
			panic(err)
		}
		req.Header.Add(httpbakery.MacaroonsHeader, base64.StdEncoding.EncodeToString(data))
	}
}

type capabilityResponse struct {
	Macaroon *macaroon.Macaroon
}

// testHandler implements AuthHTTPHandler by providing path-level
// granularity for operations.
type testHandler struct {
}

// EndpointAuth implements AuthHTTPHandler.EndpointAuth by returning an
// operation operates on the req.URL.Path entity with the HTTP method as
// an action.
//
// To test multiple-entity operations, all values of the "e" query
// parameter are also added as GET operations.
func (h testHandler) EndpointAuth(req *http.Request) []auth.Op {
	req.ParseForm()
	ops := []auth.Op{{
		Entity: "path-" + req.URL.Path,
		Action: req.Method,
	}}
	for _, entity := range req.Form["e"] {
		ops = append(ops, auth.Op{
			Entity: "path-" + entity,
			Action: "GET",
		})
	}
	return ops
}

// ServeHTTP implements http.Handler.
func (h testHandler) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	fmt.Fprintf(w, "ok %s %v", req.Method, req.URL.Path)
}

func (h testHandler) assertSuccess(c *gc.C, resp *http.Response, method, path string) {
	defer resp.Body.Close()
	data, err := ioutil.ReadAll(resp.Body)
	c.Assert(err, gc.IsNil)
	c.Assert(string(data), gc.Equals, successBody(method, path))
}

func successBody(method, path string) string {
	return fmt.Sprintf("ok %s %s", method, path)
}

// newAuthHTTPService returns a new HTTP service that serves requests from the given handler.
// The entities map holds an entry for each known entity holding a map from action to ACL.
// The checker is used to check first party caveats and may be nil.
func newAuthHTTPService(handler AuthHTTPHandler, idm auth.IdentityClient, acls ACLGetter, caveatChecker checkers.Checker) *httptest.Server {
	if caveatChecker == nil {
		caveatChecker = checkers.New()
	}
	store := newMacaroonStore()
	service := auth.NewService(auth.ServiceParams{
		CaveatChecker:  caveatChecker,
		UserChecker:    &aclUserChecker{acls},
		IdentityClient: idm,
		MacaroonStore:  store,
	})
	return httptest.NewServer(checkHTTPAuth(service, store, handler))
}

type ACL []string

type ACLMap map[string]map[string]ACL

// GetACL implements ACLGetter.GetACL by returning the ACL from
// the map.
func (e ACLMap) GetACL(_ context.Context, op auth.Op) (ACL, []checkers.Caveat, error) {
	acl := e[op.Entity][op.Action]
	logger.Infof("getting ACLs for %#v -> %#v", op, acl)
	return acl, nil, nil
}

type ACLGetterFunc func(context.Context, auth.Op) (ACL, []checkers.Caveat, error)

func (f ACLGetterFunc) GetACL(ctxt context.Context, op auth.Op) (ACL, []checkers.Caveat, error) {
	return f(ctxt, op)
}

func checkHTTPAuth(service *auth.Service, store *macaroonStore, h AuthHTTPHandler) http.Handler {
	return &httpAuthChecker{
		service: service,
		h:       h,
		store:   store,
	}
}

type httpAuthChecker struct {
	service *auth.Service
	h       AuthHTTPHandler
	store   *macaroonStore
}

func (s *httpAuthChecker) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	mss := httpbakery.RequestMacaroons(req)
	logger.Infof("%d macaroons in request", len(mss))
	authorizer := s.service.NewAuthorizer(mss)

	//	version := httpbakery.RequestVersion(req)
	if req.Method == "AUTH" {
		// Special HTTP method to ask for a capability.
		// TODO what's the best way of requesting this in in reality?
		req1 := *req
		req1.Method = req.Header.Get("AuthMethod")
		ops := s.h.EndpointAuth(&req1)
		logger.Infof("asking for capability for %#v", ops)
		conditions, err := authorizer.AllowCapability(context.TODO(), ops)
		if err != nil {
			s.writeError(w, err, req)
			return
		}
		m, err := s.store.NewMacaroon(withoutLoginOp(ops), nil)
		if err != nil {
			panic("cannot make new macaroon: " + err.Error())
		}
		for _, cond := range conditions {
			if err := m.AddFirstPartyCaveat(cond); err != nil {
				panic("cannot add first party caveat: " + err.Error())
			}
		}

		data, err := json.Marshal(capabilityResponse{m})
		if err != nil {
			s.writeError(w, err, req)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write(data)
		return
	}

	ops := s.h.EndpointAuth(req)
	logger.Infof("got ops %#v for path %v", ops, req.URL)
	authInfo, err := authorizer.Allow(req.Context(), ops)
	if err != nil {
		logger.Infof("Allow returned %#v", err)
		s.writeError(w, err, req)
		return
	}
	if authInfo.Identity != nil {
		logger.Infof("successful authorization as %q", authInfo.Identity.Id())
	} else {
		logger.Infof("successful authorization as no-one")
	}
	// Authorized OK - execute the actual handler logic.
	s.h.ServeHTTP(w, req)
	return
}

func (s *httpAuthChecker) writeError(w http.ResponseWriter, err error, req *http.Request) {
	err1, ok := errgo.Cause(err).(*auth.DischargeRequiredError)
	if !ok {
		logger.Infof("error when authorizing: %#v", err)
		// TODO permission denied error.
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	logger.Infof("got discharge-required error: %#v", err)
	if len(err1.Caveats) == 0 {
		panic("no caveats on discharge-required error!")
	}
	cookieName := "authz"
	expiry := 5 * time.Second
	if len(err1.Ops) == 1 && err1.Ops[0] == auth.LoginOp {
		cookieName = "authn"
		expiry = 24 * time.Hour
	}
	caveats := append(err1.Caveats, checkers.TimeBeforeCaveat(time.Now().Add(expiry)))
	m, err := s.store.NewMacaroon(err1.Ops, caveats)
	if err != nil {
		panic("cannot make new macaroon: " + err.Error())
	}
	// It's a discharge-required error. Write the expected httpbakery response.
	berr := httpbakery.NewDischargeRequiredErrorForRequest(m, "/", err, req)
	berr.(*httpbakery.Error).Info.CookieNameSuffix = cookieName
	// TODO we need some way of telling the client that the response shouldn't
	// be persisted.
	httprequest.ErrorMapper(httpbakery.ErrorToResponse).WriteError(w, berr)
}

type ACLGetter interface {
	GetACL(context.Context, auth.Op) (ACL, []checkers.Caveat, error)
}

type aclUserChecker struct {
	aclGetter ACLGetter
}

func (a *aclUserChecker) Allow(ctxt context.Context, id auth.Identity, ops []auth.Op) (allowed []bool, caveats []checkers.Caveat, err error) {
	defer func() {
		logger.Infof("aclUserChecker.Allow(id %#v, ops %#v -> %v, %v, %v", id, ops, allowed, caveats, err)
	}()
	u, ok := id.(idmclient.ACLUser)
	if id != nil && !ok {
		logger.Infof("warning: user %T is not ACLUser", id)
	}
	allowed = make([]bool, len(ops))
	var allCaveats []checkers.Caveat
	for i, op := range ops {
		acl, caveats, err := a.aclGetter.GetACL(ctxt, op)
		if err != nil {
			return nil, nil, errgo.Mask(err)
		}
		allCaveats = append(allCaveats, caveats...)
		ok, err := allowUser(u, acl)
		allowed[i] = ok
	}
	return allowed, allCaveats, nil
}

func allowUser(u idmclient.ACLUser, acl ACL) (bool, error) {
	if u != nil {
		return u.Allow(acl)
	}
	// No authenticated user - allow only if "everyone" is allowed",
	for _, g := range acl {
		if g == Everyone {
			return true, nil
		}
	}
	return false, nil
}
