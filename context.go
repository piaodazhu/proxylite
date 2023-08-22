package proxylite

import (
	"errors"
	"net"
	"sync"
)

type Context struct {
	tn   *tunnel
	user net.Conn
	data []byte
	kvs *sync.Map
}

func makeContext(tn *tunnel, user *net.Conn, data []byte, kvMap *sync.Map) *Context {
	if tn == nil {
		return nil
	}
	ctx := &Context{
		tn:   tn,
		data: data,
		kvs: kvMap,
	}
	if user == nil {
		ctx.user = nil
	} else {
		ctx.user = *user
	}
	return ctx
}

func (ctx *Context) AbortTunnel() error {
	if ctx.tn.innerConn == nil {
		return errors.New("cannot abort service because inner connection not exists")
	}
	return (*ctx.tn.innerConn).Close()
}

func (ctx *Context) AbortUser() error {
	if ctx.user == nil {
		return errors.New("cannot abort user because user connection not exists")
	}
	return ctx.user.Close()
}

func (ctx *Context) ServiceInfo() ServiceInfo {
	return *ctx.tn.service
}

func (ctx *Context) UserLocalAddress() net.Addr {
	return ctx.user.LocalAddr()
}

func (ctx *Context) UserRemoteAddress() net.Addr {
	return ctx.user.RemoteAddr()
}

func (ctx *Context) InnerLocalConn() net.Addr {
	if ctx.tn.innerConn == nil {
		return nil
	}
	return (*ctx.tn.innerConn).LocalAddr()
}

func (ctx *Context) InnerRemoteConn() net.Addr {
	if ctx.tn.innerConn == nil {
		return nil
	}
	return (*ctx.tn.innerConn).RemoteAddr()
}

func (ctx *Context) DataBuffer() []byte {
	return ctx.data
}

func (ctx *Context) PutValue(key, value interface{}) {
	ctx.kvs.Store(key, value)
}

func (ctx *Context) GetValue(key interface{}) (interface{}, bool) {
	return ctx.kvs.Load(key)
}
