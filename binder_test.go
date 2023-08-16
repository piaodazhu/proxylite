package proxylite

import (
	"fmt"
	"net"
	"testing"
	"time"
)

type fakeConn struct {
	remote fakeAddr
	local  fakeAddr
}

type fakeAddr struct {
	protocol string
	ip       string
	port     int
}

func (a fakeAddr) String() string {
	return a.protocol
}

func (a fakeAddr) Network() string {
	return fmt.Sprintf("%s:%d", a.ip, a.port)
}

func newFakeTunpe(protocol, localIp, remoteIp string, localPort, remotePort int) net.Conn {
	return fakeConn{
		remote: fakeAddr{
			protocol: protocol,
			ip:       remoteIp,
			port:     remotePort,
		},
		local: fakeAddr{
			protocol: protocol,
			ip:       localIp,
			port:     localPort,
		},
	}
}

func (fc fakeConn) Read(b []byte) (n int, err error) { return 0, nil }

func (fc fakeConn) Write(b []byte) (n int, err error) { return 0, nil }

var cnt int

func (fc fakeConn) Close() error                          { cnt++; return nil }
func (fc fakeConn) LocalAddr() net.Addr                   { return fc.local }
func (fc fakeConn) RemoteAddr() net.Addr                  { return fc.remote }
func (fc fakeConn) SetDeadline(t time.Time) error         { return nil }
func (fc fakeConn) SetWriteDeadline(time time.Time) error { return nil }
func (fc fakeConn) SetReadDeadline(t time.Time) error     { return nil }

func TestBinder(t *testing.T) {
	binder := newConnUidBinder(2)
	conn1 := newFakeTunpe("tcp", "127.0.0.1", "1.1.1.1", 111, 111)
	conn2 := newFakeTunpe("tcp", "127.0.0.1", "1.1.1.2", 112, 112)
	conn3 := newFakeTunpe("tcp", "127.0.0.1", "1.1.1.3", 113, 113)

	if GenConnId(&conn1) == GenConnId(&conn2) {
		t.Errorf("GenConnId 1")
	}
	var c net.Conn = conn1
	if GenConnId(&conn1) != GenConnId(&c) {
		t.Errorf("GenConnId 2")
	}
	if _, ok := binder.getUid(GenConnId(&conn1)); ok {
		t.Errorf("getUid 1")
	}
	var uid uint32
	var ok bool
	if uid, ok = binder.allocUid(GenConnId(&conn1), &conn1); !ok {
		t.Errorf("allocUid 1")
	}

	if v, ok := binder.getUid(GenConnId(&conn1)); !ok {
		t.Errorf("getUid 2")
	} else if v != uid {
		t.Errorf("getUid 3")
	}

	if v, ok := binder.getConn(uid); !ok {
		t.Errorf("getConn 1")
	} else if v != &conn1 {
		t.Errorf("getConn 2")
	}

	if ok := binder.allocConn(1, GenConnId(&conn2), &conn2); !ok {
		t.Errorf("GenConnId 1")
	}
	if v, ok := binder.getConn(1); !ok {
		t.Errorf("getConn 3")
	} else if v != &conn2 {
		t.Errorf("getConn 4")
	}

	t.Log(binder.remains)
	if _, ok := binder.allocUid(GenConnId(&conn3), &conn3); ok {
		t.Log(binder.remains)
		t.Errorf("allocUid 2")
	}
	if ok := binder.freeConnIfExists(1); !ok {
		t.Errorf("freeConnIfExists 1")
	}
	if _, ok := binder.allocUid(GenConnId(&conn3), &conn3); !ok {
		t.Errorf("allocUid 3")
	}
	if ok := binder.freeUidIfExists(GenConnId(&conn3)); !ok {
		t.Errorf("freeUidIfExists 1")
	}
	cnt = 0
	binder.closeAll()
	if cnt != 1 {
		t.Error("closeAll")
	}
}
