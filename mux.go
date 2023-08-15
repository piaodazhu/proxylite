package proxylite

import (
	"encoding/binary"
	"net"
	"sync"
	"sync/atomic"

	"github.com/cespare/xxhash/v2"
	"github.com/google/uuid"
)


func GenConnId(conn *net.Conn) uint64 {
	var tuple [256]byte
	idx := 0
	protocol := (*conn).LocalAddr().Network()
	for i := range protocol {
		tuple[idx] = protocol[i]
		idx++
	}

	local := (*conn).LocalAddr().String()
	for i := range local {
		tuple[idx] = local[i]
		idx++
	}

	remote := (*conn).RemoteAddr().String()
	for i := range remote {
		tuple[idx] = remote[i]
		idx++
	}

	return xxhash.Sum64(tuple[:idx])
}

func writeUidUnsafe(buf []byte, uid uint32) {
	binary.LittleEndian.PutUint32(buf, uid)
}

func readUidUnsafe(buf []byte) uint32 {
	return binary.LittleEndian.Uint32(buf)
}

// conn can be hash to uid, uid can map to conn
type connUidBinder struct {
	connIdToUid sync.Map
	uidToConn   sync.Map
	remains     int32
}

func newConnUidBinder(cap int) *connUidBinder {
	var maxConn int32 = 10000
	if cap > 0 {
		maxConn = int32(maxConn)
	}
	return &connUidBinder{
		connIdToUid: sync.Map{},
		uidToConn:   sync.Map{},
		remains:     maxConn,
	}
}

func (c *connUidBinder) getConn(uid uint32) (*net.Conn, bool) {
	if conn, ok := c.uidToConn.Load(uid); ok {
		return conn.(*net.Conn), true
	}
	return nil, false
}

func (c *connUidBinder) getUid(connId uint64) (uint32, bool) {
	if uid, ok := c.connIdToUid.Load(connId); ok {
		return uid.(uint32), true
	}
	return 0, false
}

func (c *connUidBinder) allocUid(connId uint64, conn *net.Conn) (uint32, bool) {
	if _, ok := c.connIdToUid.Load(connId); ok {
		return 0, false
	}

	if atomic.AddInt32(&c.remains, -1) < 0 {
		atomic.AddInt32(&c.remains, 1)
		return 0, false
	}

	uid := uuid.New().ID()
	c.connIdToUid.Store(connId, uid)
	c.uidToConn.Store(uid, conn)

	return uid, true
}

func (c *connUidBinder) freeUidIfExists(connId uint64) bool {
	if uid, ok := c.connIdToUid.LoadAndDelete(connId); ok {
		c.uidToConn.LoadAndDelete(uid)
		atomic.AddInt32(&c.remains, 1)
		return true
	}
	return false
}

func (c *connUidBinder) allocConn(uid uint32, connId uint64, conn *net.Conn) bool {
	if _, ok := c.uidToConn.Load(uid); ok {
		return false
	}

	if atomic.AddInt32(&c.remains, -1) < 0 {
		atomic.AddInt32(&c.remains, 1)
		return false
	}

	c.uidToConn.Store(uid, conn)
	c.connIdToUid.Store(connId, uid)
	return true
}

func (c *connUidBinder) freeConnIfExists(uid uint32) bool {
	if conn, ok := c.uidToConn.LoadAndDelete(uid); ok {
		c.connIdToUid.LoadAndDelete(GenConnId(conn.(*net.Conn)))
		atomic.AddInt32(&c.remains, 1)
		return true
	}
	return false
}

func (c *connUidBinder) closeAll() {
	uids := []uint32{}
	conns := []*net.Conn{}
	c.connIdToUid.Range(func(key, value interface{}) bool {
		uids = append(uids, key.(uint32))
		conns = append(conns, value.(*net.Conn))
		return true
	})
	for i := range uids {
		if c.freeConnIfExists(uids[i]) {
			(*conns[i]).Close()
		}
	}
}