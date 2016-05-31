// Package apidoc holds the shared data structure between jujuapidoc and jujuapidochtml.
package apidoc

import (
	"github.com/rogpeppe/apicompat/jsontypes"
)

// Info holds information on the Juju RPC-based API.
type Info struct {
	TypeInfo *jsontypes.Info
	Facades  []FacadeInfo
}

// FacadeInfo holds information on a particular
// version of a facade.
type FacadeInfo struct {
	Name    string
	Version int
	Doc     string `json:",omitempty"`
	Methods []Method
}

// Methods holds information on an RPC method implemented
// by a facade.
type Method struct {
	Name   string
	Doc    string          `json:",omitempty"`
	Param  *jsontypes.Type `json:",omitempty"`
	Result *jsontypes.Type `json:",omitempty"`
}
