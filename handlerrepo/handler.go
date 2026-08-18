package handlerrepo

import (
	"github.com/ayn2op/arikawa/v3/utils/handler"
)

// AddHandler is an interface for separate states to bind their handlers.
type AddHandler interface {
	AddHandler(fn any) (cancel func())
	AddSyncHandler(fn any) (cancel func())
}

var _ AddHandler = (*handler.Handler)(nil)

// Unbinder is an interface for separate states to remove their handlers.
type Unbinder interface {
	Unbind()
}

type Repository struct {
	adder  AddHandler
	cancel []func()
}

func NewRepository(adder AddHandler) *Repository {
	return &Repository{
		adder: adder,
	}
}

func (r *Repository) AddHandler(fn any) (cancel func()) {
	cancel = r.adder.AddHandler(fn)
	r.cancel = append(r.cancel, cancel)
	return
}

func (r *Repository) AddSyncHandler(fn any) (cancel func()) {
	cancel = r.adder.AddSyncHandler(fn)
	r.cancel = append(r.cancel, cancel)
	return
}

func (r *Repository) Unbind() {
	for _, fn := range r.cancel {
		fn()
	}
}
