// Package jujuconn implements some convenience methods for making connections
// to the Juju API.
package jujuconn

import (
	"sync"

	"github.com/juju/errors"
	"github.com/juju/juju/api"
	"github.com/juju/juju/juju"
	"github.com/juju/juju/jujuclient"
	"github.com/juju/persistent-cookiejar"
	"github.com/juju/utils"
	"gopkg.in/macaroon-bakery.v1/httpbakery"
)

var (
	initOnce  sync.Once
	initError error
)

type Context struct {
	store    *cacheStore
	jar      *cookiejar.Jar
	dialOpts api.DialOpts
}

func NewContext() (*Context, error) {
	initialize()
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, errors.Trace(err)
	}
	bclient := httpbakery.NewClient()
	bclient.Jar = jar
	bclient.VisitWebPage = httpbakery.OpenWebBrowser

	dialOpts := api.DefaultDialOpts()
	dialOpts.BakeryClient = bclient
	store := jujuclient.NewFileClientStore()
	cstore, err := newCacheStore(store)
	if err != nil {
		return nil, errors.Annotatef(err, "cannot make store cache")
	}
	return &Context{
		store:    cstore,
		jar:      jar,
		dialOpts: dialOpts,
	}, nil
}

func (ctxt *Context) Close() error {
	return ctxt.jar.Save()
}

func initialize() error {
	initOnce.Do(func() {
		initError = juju.InitJujuXDGDataHome()
	})
	if initError != nil {
		return errors.Annotatef(initError, "cannot initialize")
	}
	return nil
}

func DialModel(controller, model string) (api.Connection, error) {
	ctxt, err := NewContext()
	if err != nil {
		return nil, errors.Annotatef(err, "cannot make context")
	}
	defer ctxt.Close()
	return ctxt.DialModel(controller, model)
}

func DialController(controller string) (api.Connection, error) {
	ctxt, err := NewContext()
	if err != nil {
		return nil, errors.Annotatef(err, "cannot make context")
	}
	defer ctxt.Close()
	return ctxt.DialController(controller)
}

// DialModel makes an API connection to the given controller
// and model names. If the controller name is empty,
// the default controller will be used. If the model name
// is empty, the default model for the selected controller
// will be used.
//
// The model name may also be provided as a UUID.
func (ctxt *Context) DialModel(controller, model string) (api.Connection, error) {
	d, err := ctxt.ModelDialer(controller, model)
	if err != nil {
		return nil, errors.Trace(err)
	}
	return d.Dial()
}

func (ctxt *Context) ModelDialer(controller, model string) (*Dialer, error) {
	if controller == "" {
		c, err := ctxt.store.origStore.CurrentController()
		if err != nil {
			return nil, errors.Annotatef(err, "cannot get current controller")
		}
		controller = c
	}
	if model == "" {
		m, err := ctxt.store.origStore.CurrentModel(controller)
		if err != nil {
			return nil, errors.Annotatef(err, "cannot get current model")
		}
		model = m
	}
	modelUUID := model
	if !utils.IsValidUUIDString(model) {
		md, err := ctxt.store.origStore.ModelByName(controller, model)
		if err != nil {
			return nil, errors.Annotatef(err, "cannot get model")
		}
		modelUUID = md.ModelUUID
	}
	return ctxt.dialer(controller, modelUUID)
}

// DialController makes a controller-only connection to the controller
// with the given name. If the name is empty, the current controller
// will be used.
func (ctxt *Context) DialController(controller string) (api.Connection, error) {
	d, err := ctxt.ControllerDialer(controller)
	if err != nil {
		return nil, errors.Trace(err)
	}
	return d.Dial()
}

func (ctxt *Context) ControllerDialer(controller string) (*Dialer, error) {
	if controller == "" {
		c, err := ctxt.store.origStore.CurrentController()
		if err != nil {
			return nil, errors.Annotatef(err, "cannot get current controller")
		}
		controller = c
	}
	return ctxt.dialer(controller, "")
}

func (ctxt *Context) dialer(controller, modelUUID string) (*Dialer, error) {
	acct, err := ctxt.store.AccountDetails(controller)
	if err != nil && !errors.IsNotFound(err) {
		return nil, errors.Annotatef(err, "cannot get account for controller %q", controller)
	}
	return &Dialer{
		ctxt:       ctxt,
		controller: controller,
		account:    acct,
		modelUUID:  modelUUID,
	}, nil
}

type Dialer struct {
	ctxt       *Context
	controller string
	account    *jujuclient.AccountDetails
	modelUUID  string
}

func (d *Dialer) Dial() (api.Connection, error) {
	c, err := juju.NewAPIConnection(juju.NewAPIConnectionParams{
		ControllerName: d.controller,
		Store:          d.ctxt.store,
		OpenAPI:        api.Open,
		DialOpts:       d.ctxt.dialOpts,
		AccountDetails: d.account,
		ModelUUID:      d.modelUUID,
	})
	if err != nil {
		return nil, errors.Trace(err)
	}
	return c, nil
}

// cacheStore avoids disk access when dialing.
// It implements only those methods required by
// juju.NewAPIConnection.
type cacheStore struct {
	mu sync.Mutex
	jujuclient.ClientStore
	origStore       jujuclient.ClientStore
	accounts        map[string]jujuclient.AccountDetails
	updatedAccounts map[string]bool
	controllers     map[string]jujuclient.ControllerDetails
}

func newCacheStore(store jujuclient.ClientStore) (*cacheStore, error) {
	controllers, err := store.AllControllers()
	if err != nil {
		return nil, errors.Trace(err)
	}
	accounts := make(map[string]jujuclient.AccountDetails)
	for controller := range controllers {
		acct, err := store.AccountDetails(controller)
		if err != nil {
			if !errors.IsNotFound(err) {
				return nil, errors.Annotatef(err, "cannot get account details for %q", controller)
			}
		} else {
			accounts[controller] = *acct
		}
	}
	return &cacheStore{
		accounts:        accounts,
		origStore:       store,
		controllers:     controllers,
		updatedAccounts: make(map[string]bool),
	}, nil
}

func (s *cacheStore) AccountDetails(c string) (*jujuclient.AccountDetails, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	acct, ok := s.accounts[c]
	if !ok {
		return nil, errors.NotFoundf("account for controller %q", c)
	}
	return &acct, nil
}

func (s *cacheStore) ControllerByName(c string) (*jujuclient.ControllerDetails, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	controller, ok := s.controllers[c]
	if !ok {
		return nil, errors.NotFoundf("controller %q", c)
	}
	return &controller, nil
}

func (s *cacheStore) UpdateAccount(c string, details jujuclient.AccountDetails) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.accounts[c] = details
	s.updatedAccounts[c] = true
	return nil
}

func (s *cacheStore) save() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for c := range s.updatedAccounts {
		if err := s.origStore.UpdateAccount(c, s.accounts[c]); err != nil {
			return errors.Annotatef(err, "cannot update account %q", c)
		}
	}
	s.updatedAccounts = make(map[string]bool)
	return nil
}
