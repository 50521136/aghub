package aghhttp

import (
	"net/http"
)

// Registrar registers an HTTP handler for a method and path.
//
// TODO(s.chzhen):  Implement [httputil.Router].
type Registrar interface {
	Register(method, path string, h http.HandlerFunc)
}

// EmptyRegistrar is an implementation of [Registrar] that does nothing.
type EmptyRegistrar struct{}

// type check
var _ Registrar = EmptyRegistrar{}

// Register implements the [Registrar] interface.
func (EmptyRegistrar) Register(_, _ string, _ http.HandlerFunc) {}

// WrapFunc is a wrapper function that builds an HTTP handler for a route.
type WrapFunc func(method string, h http.HandlerFunc) (wrapped http.Handler)

// DefaultRegistrar is an implementation of [Registrar] that registers handlers
// after applying a user-provided wrapper function.
type DefaultRegistrar struct {
	mux    *http.ServeMux
	wrapFn WrapFunc
}

// NewDefaultRegistrar returns a new properly initialized *DefaultRegistrar.
// mux and wrap must not be nil.
func NewDefaultRegistrar(mux *http.ServeMux, wrap WrapFunc) (r *DefaultRegistrar) {
	return &DefaultRegistrar{
		mux:    mux,
		wrapFn: wrap,
	}
}

// type check
var _ Registrar = (*DefaultRegistrar)(nil)

// NewPlainRegistrar returns a registrar that registers the handlers as they
// are, without any middleware.  It is meant for the interfaces that
// authenticate on their own, such as the user portal, whose sessions must not
// be mixed up with the administrator ones.
func NewPlainRegistrar(mux *http.ServeMux) (r *DefaultRegistrar) {
	return NewDefaultRegistrar(mux, func(_ string, h http.HandlerFunc) (wrapped http.Handler) {
		return h
	})
}

// Register implements the [Registrar] interface.
func (r *DefaultRegistrar) Register(method, path string, h http.HandlerFunc) {
	wrapped := r.wrapFn(method, h)
	r.mux.Handle(path, wrapped)
}
