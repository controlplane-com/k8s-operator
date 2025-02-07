package cpln

import (
	"context"
	"time"
)

type Context interface {
	context.Context
	Org() string
	Gvc() string
	Token() string
}

func NewContext(ctx context.Context, org, gvc, token string) Context {
	return &cplnContext{
		ctx:   ctx,
		gvc:   gvc,
		token: token,
		org:   org,
	}
}

type cplnContext struct {
	org   string
	gvc   string
	token string
	ctx   context.Context
}

func (c *cplnContext) Org() string {
	return c.org
}

func (c *cplnContext) Gvc() string {
	return c.gvc
}

func (c *cplnContext) Token() string {
	return c.token
}

func (c *cplnContext) Deadline() (deadline time.Time, ok bool) {
	return c.ctx.Deadline()
}

func (c *cplnContext) Done() <-chan struct{} {
	return c.ctx.Done()
}

func (c *cplnContext) Err() error {
	return c.ctx.Err()
}

func (c *cplnContext) Value(key any) any {
	return c.ctx.Value(key)
}
