package proxylite

import "testing"

func TestContextNil(t *testing.T) {
	data := []byte{'a'}
	ctx := makeContext(nil, nil, data, nil)
	if ctx.AbortTunnel() == nil {
		t.Error("AbortTunnel")
	}
	if ctx.AbortUser() == nil {
		t.Error("AbortUser")
	}
	if ctx.DataBuffer() == nil {
		t.Error("DataBuffer")
	}

	blankInfo := ServiceInfo{}
	if ctx.ServiceInfo() != blankInfo {
		t.Error("ServiceInfo")
	}
	if ctx.UserLocalAddress() != nil {
		t.Error("UserLocalAddress")
	}
	if ctx.UserRemoteAddress() != nil {
		t.Error("UserRemoteAddress")
	}
	if ctx.InnerLocalConn() != nil {
		t.Error("InnerLocalConn")
	}
	if ctx.InnerRemoteConn() != nil {
		t.Error("InnerRemoteConn")
	}
	if _, ok := ctx.GetValue("k"); ok {
		t.Error("GetValue")
	}
}
