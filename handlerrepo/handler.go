package handlerrepo

import (
	"github.com/ayn2op/arikawa/v3/utils/handler"
)

// Unbinder is an interface for separate states to remove their handlers.
type Unbinder interface {
	Unbind()
}

// Repository wraps a handler and keeps track of the handlers added through it, so that they can all be removed at once with Unbind.
type Repository struct {
	handler *handler.Handler
	cancel  []func()
}

func NewRepository(h *handler.Handler) *Repository {
	return &Repository{
		handler: h,
	}
}

func (r *Repository) AddHandler[E any](fn func(E)) (cancel func()) {
	cancel = r.handler.AddHandler(fn)
	r.cancel = append(r.cancel, cancel)
	return
}

func (r *Repository) AddSyncHandler[E any](fn func(E)) (cancel func()) {
	cancel = r.handler.AddSyncHandler(fn)
	r.cancel = append(r.cancel, cancel)
	return
}

func (r *Repository) Unbind() {
	for _, fn := range r.cancel {
		fn()
	}
}
