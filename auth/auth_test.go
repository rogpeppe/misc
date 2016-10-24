package auth_test

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"net/http/httptest"

	"github.com/juju/httprequest"
	"github.com/juju/idmclient"
	"github.com/juju/idmclient/idmtest"
	"golang.org/x/net/context"
	gc "gopkg.in/check.v1"
	errgo "gopkg.in/errgo.v1"
	macaroon1 "gopkg.in/macaroon.v1"
	"gopkg.in/macaroon.v2-unstable"

	httpbakery1 "gopkg.in/macaroon-bakery.v1/httpbakery"
	"gopkg.in/macaroon-bakery.v2-unstable/auth"
	"gopkg.in/macaroon-bakery.v2-unstable/bakery"
	"gopkg.in/macaroon-bakery.v2-unstable/bakery/checkers"
	"gopkg.in/macaroon-bakery.v2-unstable/httpbakery"
)

// TODO move idmclient to latest bakery version so we can avoid
// double dependencies above.

type authSuite struct{}

var _ = gc.Suite(&authSuite{})

// AuthHTTPHandler represents an HTTP handler that can be queried
// for authorization information.
type AuthHTTPHandler interface {
	http.Handler
	// EndpointAuth returns the operations and caveats required by the
	// endpoint implied by the given request.
	EndpointAuth(req *http.Request) ([]auth.Op, []checkers.Caveat)
}

func (*authSuite) TestAuthorizeWithHTTPBakery(c *gc.C) {
	h := testHandler{}
	svc := newAuthHTTPService(h, map[string]map[string]auth.ACL{
		"path-/": {
			"GET": {auth.Everyone},
		},
	}, nil)
	defer svc.Close()

	// Anyone should be able to access /, so try it without a bakery client.
	resp, err := http.Get(svc.URL + "/")
	c.Assert(err, gc.IsNil)
	h.assertSuccess(c, resp, "GET", "/")
}

func (*authSuite) TestEndpointWithAuthenticationRequired(c *gc.C) {
	h := testHandler{}
	svc := newAuthHTTPService(h, map[string]map[string]auth.ACL{
		"path-/bob": {
			"PUT": {"bob"},
		},
	}, nil)
	defer svc.Close()

	client := svc.idm.Client("bob")
	req, err := http.NewRequest("PUT", svc.URL+"/bob", nil)
	c.Assert(err, gc.IsNil)
	resp, err := client.Do(req)
	c.Assert(err, gc.IsNil)
	h.assertSuccess(c, resp, "PUT", "/bob")
}

func (*authSuite) TestAuthorizeMultipleEntities(c *gc.C) {
	h := testHandler{}
	svc := newAuthHTTPService(h, map[string]map[string]auth.ACL{
		"path-/": {
			"GET": {auth.Everyone},
		},
		"path-/bob": {
			"GET": {"bob"},
			"PUT": {"bob"},
		},
		"path-/alice": {
			"GET": {"alice", "bob"},
			"PUT": {"alice"},
		},
	}, nil)
	defer svc.Close()

	client := svc.idm.Client("bob")
	req, err := http.NewRequest("GET", svc.URL+"/bob?e=/alice", nil)
	c.Assert(err, gc.IsNil)
	resp, err := client.Do(req)
	c.Assert(err, gc.IsNil)
	h.assertSuccess(c, resp, "GET", "/bob")
}

func (*authSuite) TestCapability(c *gc.C) {
	h := testHandler{}
	svc := newAuthHTTPService(h, map[string]map[string]auth.ACL{
		"path-/bob": {
			"GET": {"bob"},
		},
	}, nil)
	defer svc.Close()

	ms := getCapability(c, svc.idm.Client("bob"), "GET", svc.URL+"/bob")

	// With this capability, we should be able to do a get without any other
	// authorization.
	data, err := json.Marshal(ms)
	c.Assert(err, gc.IsNil)
	value := base64.StdEncoding.EncodeToString(data)

	req, err := http.NewRequest("GET", svc.URL+"/bob", nil)
	c.Assert(err, gc.IsNil)
	req.Header.Set(httpbakery.MacaroonsHeader, value)

	resp, err := http.DefaultClient.Do(req)
	c.Assert(err, gc.IsNil)
	h.assertSuccess(c, resp, "GET", "/bob")
}

func (*authSuite) TestCapabilityMultipleEntities(c *gc.C) {
	h := testHandler{}
	svc := newAuthHTTPService(h, map[string]map[string]auth.ACL{
		"path-/bob": {
			"GET": {"bob"},
			"PUT": {"bob"},
		},
		"path-/alice": {
			"GET": {"alice", "bob"},
			"PUT": {"alice"},
		},
	}, nil)
	defer svc.Close()

	url := svc.URL + "/bob?e=/alice"
	ms := getCapability(c, svc.idm.Client("bob"), "GET", url)

	// With this capability, we should be able to do a get without any other
	// authorization.
	resp := doWithCapabilities(c, http.DefaultClient, "GET", url, ms)
	h.assertSuccess(c, resp, "GET", "/bob")
}

func (*authSuite) TestMultipleCapabilities(c *gc.C) {
	h := testHandler{}
	svc := newAuthHTTPService(h, map[string]map[string]auth.ACL{
		"path-/bob": {
			"GET": {"bob"},
		},
		"path-/alice": {
			"GET": {"alice"},
		},
	}, nil)
	defer svc.Close()

	bobCap := getCapability(c, svc.idm.Client("bob"), "GET", svc.URL+"/bob")
	aliceCap := getCapability(c, svc.idm.Client("alice"), "GET", svc.URL+"/alice")

	// We should be able to use both capabilities to act
	// on two endpoints at once.
	resp := doWithCapabilities(c, http.DefaultClient, "GET", svc.URL+"/bob?e=/alice", bobCap, aliceCap)
	h.assertSuccess(c, resp, "GET", "/bob")
}

func (*authSuite) TestCombineCapabilities(c *gc.C) {
	h := testHandler{}
	svc := newAuthHTTPService(h, map[string]map[string]auth.ACL{
		"path-/bob": {
			"GET": {"bob"},
		},
		"path-/alice": {
			"GET": {"alice"},
		},
	}, nil)
	defer svc.Close()

	bobCap := getCapability(c, svc.idm.Client("bob"), "GET", svc.URL+"/bob")
	aliceCap := getCapability(c, svc.idm.Client("alice"), "GET", svc.URL+"/alice")

	// We should be able to combine both capabilities into a single one.
	bothCap := getCapability(c, httpbakery1.NewClient(), "GET", svc.URL+"/bob?e=/alice", bobCap, aliceCap)

	c.Logf("bothCap id %q", bothCap[0].Id())

	// We should be able to use the new capability to act as both endpoints at once.
	resp := doWithCapabilities(c, http.DefaultClient, "GET", svc.URL+"/bob?e=/alice", bothCap)
	h.assertSuccess(c, resp, "GET", "/bob")

	// We should also be able to use it to act on one of the entities only.
	resp = doWithCapabilities(c, http.DefaultClient, "GET", svc.URL+"/alice", bothCap)
	h.assertSuccess(c, resp, "GET", "/alice")
}

func (*authSuite) TestAuthnWithAuthz(c *gc.C) {
	h := testHandler{}
	svc := newAuthHTTPService(h, map[string]map[string]auth.ACL{
		"path-/bob": {
			"GET": {"bob"},
		},
		"path-/alice": {
			"GET": {"alice"},
		},
	}, nil)
	defer svc.Close()
	aliceCap := getCapability(c, svc.idm.Client("alice"), "GET", svc.URL+"/alice")

	resp := doWithCapabilities(c, svc.idm.Client("bob"), "GET", svc.URL+"/bob?e=/alice", aliceCap)
	h.assertSuccess(c, resp, "GET", "/bob")
}

type httpDoer interface {
	Do(*http.Request) (*http.Response, error)
}

func doWithCapabilities(c *gc.C, client httpDoer, method, url string, mss ...macaroon1.Slice) *http.Response {
	req, err := http.NewRequest(method, url, nil)
	c.Assert(err, gc.IsNil)
	addCapabilities(req, mss)

	resp, err := client.Do(req)
	c.Assert(err, gc.IsNil)
	return resp
}

func getCapability(c *gc.C, client *httpbakery1.Client, method, url string, mss ...macaroon1.Slice) macaroon1.Slice {
	req, err := http.NewRequest("AUTH", url, nil)
	c.Assert(err, gc.IsNil)
	req.Header.Set("AuthMethod", method)
	addCapabilities(req, mss)
	resp, err := client.Do(req)
	c.Assert(err, gc.IsNil)
	defer resp.Body.Close()
	data, err := ioutil.ReadAll(resp.Body)
	c.Assert(err, gc.IsNil)
	var capResp capabilityResponse

	err = json.Unmarshal(data, &capResp)
	c.Assert(err, gc.IsNil, gc.Commentf("response body: %q", data))
	c.Assert(capResp.Macaroon, gc.NotNil)
	c.Logf("successfully got capability")

	ms, err := client.DischargeAll(capResp.Macaroon)
	c.Assert(err, gc.IsNil)
	return ms
}

func addCapabilities(req *http.Request, mss []macaroon1.Slice) {
	for _, ms := range mss {
		data, err := json.Marshal(ms)
		if err != nil {
			panic(err)
		}
		req.Header.Add(httpbakery.MacaroonsHeader, base64.StdEncoding.EncodeToString(data))
	}
}

type capabilityResponse struct {
	Macaroon *macaroon1.Macaroon
}

// testHandler implements a AuthHTTPHandler by providing path-level
// granularity for operations.
type testHandler struct {
}

// EndpointAuth implements AuthHTTPHandler.EndpointAuth by returning an
// operation operates on the req.URL.Path entity with the HTTP method as
// an action.
//
// To test multiple-entity operations, all values of the "e" query
// parameter are also added as GET operations.
func (h testHandler) EndpointAuth(req *http.Request) ([]auth.Op, []checkers.Caveat) {
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
	return ops, nil
}

// ServeHTTP implements http.Handler.
func (h testHandler) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	fmt.Fprintf(w, "ok %s %v", req.Method, req.URL.Path)
}

func (h testHandler) assertSuccess(c *gc.C, resp *http.Response, method, path string) {
	defer resp.Body.Close()
	data, err := ioutil.ReadAll(resp.Body)
	c.Assert(err, gc.IsNil)
	c.Assert(string(data), gc.Equals, fmt.Sprintf("ok %s %s", method, path))
}

type authHTTPService struct {
	*httptest.Server
	handler  AuthHTTPHandler
	entities map[string]map[string]auth.ACL
	idm      *identityService
}

// newAuthHTTPService returns a new HTTP service that serves requests from the given handler.
// The entities map holds an entry for each known entity holding a map from action to ACL.
// The checker is used to check first party caveats and may be nil.
func newAuthHTTPService(handler AuthHTTPHandler, entities map[string]map[string]auth.ACL, caveatChecker checkers.Checker) *authHTTPService {
	key, err := bakery.GenerateKey()
	if err != nil {
		panic(err)
	}
	svc := &authHTTPService{
		entities: entities,
		idm:      newIdentityService(),
	}
	locator := httpbakery.NewThirdPartyLocator(nil, nil)
	locator.AllowInsecure()
	checker := auth.NewChecker(auth.Params{
		GetACLs:         svc.getACLs,
		CaveatChecker:   caveatChecker,
		Key:             key,
		IdentityService: svc.idm,
		Location:        "service",
		Locator:         locator,
	})
	svc.Server = httptest.NewServer(checkHTTPAuth(checker, handler))
	return svc
}

func (svc *authHTTPService) getACLs(_ context.Context, ops []auth.Op) ([]auth.ACL, error) {
	acls := make([]auth.ACL, len(ops))
	for i, op := range ops {
		acls[i] = svc.entities[op.Entity][op.Action]
	}
	log.Printf("getting ACLs for %#v -> %#v", ops, acls)
	return acls, nil
}

func checkHTTPAuth(checker *auth.Checker, h AuthHTTPHandler) http.Handler {
	return &httpAuthChecker{
		checker: checker,
		h:       h,
	}
}

type httpAuthChecker struct {
	checker *auth.Checker
	h       AuthHTTPHandler
}

func (s *httpAuthChecker) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	// Find information out about the request.

	version := httpbakery.RequestVersion(req)
	mss := httpbakery.RequestMacaroons(req)
	if req.Method == "AUTH" {
		// Special HTTP method to ask for a capability.
		// TODO what's the best way of requesting this in in reality?
		req1 := *req
		req1.Method = req.Header.Get("AuthMethod")
		ops, caveats := s.h.EndpointAuth(&req1)
		m, _, err := s.checker.Capability(context.TODO(), mss, version, ops, caveats)
		if err != nil {
			writeError(w, err, req)
			return
		}
		// TODO use single type for request and response when idm client
		// uses macaroon.v2.
		data, err := json.Marshal(struct {
			Macaroon *macaroon.Macaroon
		}{m})
		if err != nil {
			writeError(w, err, req)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write(data)
		return
	}

	ops, caveats := s.h.EndpointAuth(req)

	// Authorize the request.
	authInfo, err := s.checker.Authorize(context.TODO(), mss, version, ops, caveats)
	if err != nil {
		writeError(w, err, req)
		return
	}
	if authInfo.User != nil {
		log.Printf("successful authorization as %q", authInfo.User.Id())
	} else {
		log.Printf("successful authorization as no-one")
	}
	// Authorized OK - execute the actual handler logic.
	s.h.ServeHTTP(w, req)
	return
}

func writeError(w http.ResponseWriter, err error, req *http.Request) {
	err1, ok := errgo.Cause(err).(*auth.DischargeRequiredError)
	if !ok {
		log.Printf("error when authorizing: %#v", err)
		// TODO permission denied error.
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	log.Printf("got discharge-required error: %#v", err)
	// It's a discharge-required error. Write the expected httpbakery response.
	berr := httpbakery.NewDischargeRequiredErrorForRequest(err1.Macaroon, "/", err, req)
	if err1.Authenticator {
		berr.(*httpbakery.Error).Info.CookieNameSuffix = "authn"
	} else {
		berr.(*httpbakery.Error).Info.CookieNameSuffix = "authz"
		// TODO we need some way of telling the client that the response shouldn't
		// be persisted.
	}
	httprequest.ErrorMapper(httpbakery.ErrorToResponse).WriteError(w, berr)
}

func (s *authHTTPService) Close() {
	s.Server.Close()
	s.idm.Close()
}

type identityService struct {
	*idmtest.Server
	permChecker *idmclient.PermChecker
}

func newIdentityService() *identityService {
	srv := idmtest.NewServer()

	client := idmclient.New(idmclient.NewParams{
		BaseURL: srv.URL.String(),
		Client:  srv.Client("auth-user"),
	})
	return &identityService{
		Server:      srv,
		permChecker: idmclient.NewPermChecker(client, 0),
	}
}

func (svc *identityService) IdentityCaveat(*bakery.PublicKey) checkers.Caveat {
	return checkers.Caveat{
		Location:  svc.Server.URL.String(),
		Condition: "is-authenticated-user",
	}
}

func (svc *identityService) DeclaredUser(declared checkers.Declared, _ *bakery.KeyPair) (auth.User, error) {
	id, ok := declared["username"]
	if !ok {
		return nil, errgo.New("no username declared")
	}
	if id == "" {
		return nil, errgo.New("empty username declared")
	}
	return &identityUser{
		id:  id,
		svc: svc,
	}, nil
}

type identityUser struct {
	id  string
	svc *identityService
}

func (u *identityUser) Id() string {
	return u.id
}

func (u *identityUser) Domain() string {
	return ""
}

func (u *identityUser) PublicKey() *bakery.PublicKey {
	return nil
}

func (u *identityUser) Allow(ctxt context.Context, acl auth.ACL) (bool, error) {
	return u.svc.permChecker.Allow(u.id, acl)
}

// TODO parse actual encrypted user info:
//	version
//	public key
//
//	fields := strings.Fields(s)
//	// user-id domain info-URL
//	if len(fields) != 3 {
//		// TODO is it perhaps a security flaw to return the
//		// decrypted string in the error. Perhaps we should
//		// just log it instead.
//		return nil, errgo.Newf("wrong field count in username info %q", s)
//	}
//	u, err := url.Parse(fields[2])
//	if err != nil {
//		return nil, errgo.Newf("canot parse URL %q in username info", fields[2])
//	}
//	return &usernameInfo{
//		id: fields[0],
//		domain: fields[1],
//		infoURL: u,
//	}, nil
//}
