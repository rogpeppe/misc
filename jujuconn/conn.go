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

// DialModel makes an API connection to the given controller
// and model names. If the controller name is empty,
// the default controller will be used. If the model name
// is empty, the default model for the selected controller
// will be used.
//
// The model name may also be provided as a UUID.
func DialModel(controller, model string) (api.Connection, error) {
	if err := initialize(); err != nil {
		return nil, errors.Trace(err)
	}
	store := jujuclient.NewFileClientStore()
	if controller == "" {
		c, err := store.CurrentController()
		if err != nil {
			return nil, errors.Annotatef(err, "cannot get current controller")
		}
		controller = c
	}
	if model == "" {
		m, err := store.CurrentModel(controller)
		if err != nil {
			return nil, errors.Annotatef(err, "cannot get current model")
		}
		model = m
	}
	modelUUID := model
	if !utils.IsValidUUIDString(model) {
		md, err := store.ModelByName(controller, model)
		if err != nil {
			return nil, errors.Annotatef(err, "cannot get model")
		}
		modelUUID = md.ModelUUID
	}
	c, err := dial(store, controller, modelUUID)
	if err != nil {
		return nil, errors.Trace(err)
	}
	return c, nil
}

// DialController makes a controller-only connection to the controller
// with the given name. If the name is empty, the current controller
// will be used.
func DialController(controller string) (api.Connection, error) {
	if err := initialize(); err != nil {
		return nil, errors.Trace(err)
	}
	store := jujuclient.NewFileClientStore()
	if controller == "" {
		c, err := store.CurrentController()
		if err != nil {
			return nil, errors.Annotatef(err, "cannot get current controller")
		}
		controller = c
	}
	c, err := dial(store, controller, "")
	if err != nil {
		return nil, errors.Trace(err)
	}
	return c, nil
}

func dial(store jujuclient.ClientStore, controller, modelUUID string) (api.Connection, error) {
	acct, err := store.AccountDetails(controller)
	if err != nil && !errors.IsNotFound(err) {
		return nil, errors.Annotatef(err, "cannot get account for controller %q", controller)
	}
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, errors.Trace(err)
	}
	bclient := httpbakery.NewClient()
	bclient.Jar = jar
	bclient.VisitWebPage = httpbakery.OpenWebBrowser

	dialOpts := api.DefaultDialOpts()
	dialOpts.BakeryClient = bclient

	c, err := juju.NewAPIConnection(juju.NewAPIConnectionParams{
		ControllerName: controller,
		Store:          store,
		OpenAPI:        api.Open,
		DialOpts:       dialOpts,
		AccountDetails: acct,
		ModelUUID:      modelUUID,
	})
	if err != nil {
		return nil, errors.Trace(err)
	}
	return c, nil
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
