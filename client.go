package proxylite

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"sort"
	"sync"

	log "github.com/sirupsen/logrus"
)

// RegisterEntry entry to discribe a single service registration
type RegisterEntry struct {
	// Basic Info
	Info RegisterInfo
	// Control Info
	Ctrl ControlInfo
	// Cancel function
	Cancel func()
	// Done channel
	Done <-chan struct{}
}

// ProxyLiteClient inner client that forwards traffic between inner service and proxy server server.
type ProxyLiteClient struct {
	ready          bool
	serverAddr     string
	lock           sync.RWMutex
	avaliablePorts map[uint32]struct{}
	registered     map[uint32]*RegisterEntry
	logger         *log.Logger
}

// NewProxyLiteClient Create a inner client binding with a proxy server.
func NewProxyLiteClient(serverAddr string) *ProxyLiteClient {
	client := &ProxyLiteClient{
		ready:          false,
		serverAddr:     serverAddr,
		avaliablePorts: map[uint32]struct{}{},
		registered:     map[uint32]*RegisterEntry{},
		logger:         log.New(),
	}
	// client.logger.SetReportCaller(true)

	ports, err := AskFreePort(serverAddr)
	if err != nil {
		return client
	}
	client.ready = true
	for _, port := range ports {
		client.avaliablePorts[port] = struct{}{}
	}
	return client
}

// AvaliablePorts Get avaliable ports from proxy server.
func (c *ProxyLiteClient) AvaliablePorts() ([]uint32, bool) {
	ports, err := AskFreePort(c.serverAddr)
	if err != nil {
		c.ready = false
		return nil, false
	}
	c.ready = true
	for _, port := range ports {
		c.avaliablePorts[port] = struct{}{}
	}
	return ports, true
}

// AnyPort Get a random avaliable port from proxy server.
func (c *ProxyLiteClient) AnyPort() (uint32, bool) {
	if c.ready {
		for port := range c.avaliablePorts {
			return port, true
		}
		return 0, false
	}

	_, ok := c.AvaliablePorts()
	if !ok {
		return 0, false
	}

	for port := range c.avaliablePorts {
		return port, true
	}
	return 0, false
}

// ActiveServices Discover all active services from proxy server.
func (c *ProxyLiteClient) ActiveServices() ([]ServiceInfo, error) {
	return DiscoverServices(c.serverAddr)
}

func register(conn net.Conn, info RegisterInfo, ctrl ControlInfo) error {
	req := RegisterServiceReq{
		Info: info,
		Ctrl: ctrl,
	}
	raw, _ := json.Marshal(req)
	err := sendMessage(conn, TypeRegisterServiceReq, raw)
	if err != nil {
		return err
	}

	mtype, data, err := recvMessage(conn)
	if err != nil {
		return err
	}
	if mtype != TypeRegisterServiceRsp {
		return errors.New("protocol mismatch")
	}

	rsp := RegisterServiceRsp{}
	err = json.Unmarshal(data, &rsp)
	if err != nil {
		return err
	}
	if rsp.Code != 0 {
		return fmt.Errorf("register failed: Code=%d", rsp.Code)
	}
	return nil
}

// SetLogger Set customized logrus logger for the inner client.
func (c *ProxyLiteClient) SetLogger(logger *log.Logger) {
	c.logger = logger
}

// RegisterInnerService Register inner server to proxy server's outer port.
func (c *ProxyLiteClient) RegisterInnerService(info RegisterInfo, ctrl ControlInfo) (func(), chan struct{}, error) {
	if !c.ready {
		return nil, nil, errors.New("client not ready")
	}
	c.lock.Lock()
	defer c.lock.Unlock()
	if _, exists := c.registered[info.OuterPort]; exists {
		return nil, nil, errors.New("ports is occupied")
	}

	serverConn, err := net.Dial("tcp", c.serverAddr)
	if err != nil {
		c.ready = false
		return nil, nil, err
	}

	// Register the service must return nil.
	err = register(serverConn, info, ctrl)
	c.logTunnelMessage(info.Name, "REGISTER", fmt.Sprint("err=", err))
	if err != nil {
		serverConn.Close()
		return nil, nil, err
	}

	binder := newConnUidBinder(0)
	doOnce := sync.Once{}
	done := make(chan struct{}, 1)

	var totalOut, totalIn uint64
	closeTunnel := func() {
		serverConn.Close()
		binder.closeAll()
		done <- struct{}{}
		close(done)
		c.logTunnelMessage(info.Name, "CLOSE", fmt.Sprintf("tunnel closed, Out %dB, In %dB", totalOut, totalIn))
	}

	cancelFunc := func() {
		doOnce.Do(closeTunnel)
	}

	// Now we can add the new entry to registered table.
	c.registered[info.OuterPort] = &RegisterEntry{
		Info:   info,
		Ctrl:   ctrl,
		Cancel: cancelFunc,
		Done:   done,
	}

	readFromService := func(inner net.Conn, uid uint32) {
		buf := make([]byte, 32768)
		var n int
		var err error
		var serviceEnd bool = false
		for !serviceEnd {
			if inner == nil {
				// no inner service. send user close
				c.logTunnelMessage(info.Name, "NOSRV", fmt.Sprintf("service not avaliable, uid[%d] [%v]", uid, err))
				n = 0
				writeUidWithCloseUnsafe(buf[8:], uid)
				serviceEnd = true
			} else {
				// has inner serivce. read from service
				n, err = inner.Read(buf[16:]) // type(4) + length(4) + uid(4) + close(4)
				if err != nil {
					// read this service failed. send user close
					writeUidWithCloseUnsafe(buf[8:], uid)
					serviceEnd = true
				} else {
					writeUidUnsafe(buf[8:], uid)
				}
			}
			err = sendMessageOnBuffer(serverConn, TypeDataSegment, buf, n+8) // uid(4) + close(4)
			if err != nil {
				break
			}

			totalOut += uint64(n)
		}
		if binder.freeConnIfExists(uid) {
			inner.Close()
			c.logTunnelMessage(info.Name, "FINISH", fmt.Sprintf("one user finish, uid[%d] [%v]", uid, err))
		}
	}

	go func() {
		buf := make([]byte, 32768)
		var mtype, n int
		var data []byte
		var uid uint32
		var innerConn *net.Conn
		var ok, alive bool
		var err error
		for {
			mtype, data, err = recvMessageWithBuffer(serverConn, buf)
			if err != nil || mtype != TypeDataSegment {
				c.logger.Error(err, mtype)
				break
			}
			uid, alive = readUidUnsafe(data)
			if innerConn, ok = binder.getConn(uid); !ok {
				// Blocking. can be optimized.
				newConn, err := net.Dial("tcp", info.InnerAddr)
				if err != nil {
					// should close this client. leave it to below process.
					readFromService(nil, uid)
					continue
				}
				innerConn = &newConn
				if !binder.allocConn(uid, GenConnId(innerConn), innerConn) {
					// not suppose to reach here because proxy server will do concurrency control
					break
				}
				go readFromService(*innerConn, uid)
			} else if !alive {
				if binder.freeConnIfExists(uid) {
					(*innerConn).Close()
					c.logTunnelMessage(info.Name, "FINISH", fmt.Sprintf("one user finish, uid[%d] [%v]", uid, err))
				}
			}

			n, err = (*innerConn).Write(data[8:])
			if err != nil {
				if binder.freeConnIfExists(uid) {
					(*innerConn).Close()
					c.logTunnelMessage(info.Name, "FINISH", fmt.Sprintf("one user finish, uid[%d] [%v]", uid, err))
				}
			}
			totalIn += uint64(n)
		}

		doOnce.Do(closeTunnel)

		c.logTunnelMessage(info.Name, "UNREGISTER", "service unregister")
		// Delete the service mapping from table.
		c.lock.Lock()
		delete(c.registered, info.OuterPort)
		c.lock.Unlock()
	}()
	return cancelFunc, done, nil
}

// GetRegisterEntryByName Get RegisterEntry
func (c *ProxyLiteClient) GetRegisterEntryByName(name string) (*RegisterEntry, bool) {
	c.lock.RLock()
	defer c.lock.RUnlock()
	for _, entry := range c.registered {
		if entry.Info.Name == name {
			return entry, true
		}
	}
	return nil, false
}

// GetRegisterEntryByPort Get RegisterEntry
func (c *ProxyLiteClient) GetRegisterEntryByPort(port int) (*RegisterEntry, bool) {
	c.lock.RLock()
	defer c.lock.RUnlock()
	entry, exists := c.registered[uint32(port)]
	return entry, exists
}

func (c *ProxyLiteClient) logTunnelMessage(service, header, msg string) {
	c.logger.Infof("[%s] [%s] %s", service, header, msg)
}

// AskFreePort Ask avaliable free port from proxy server with given address.
func AskFreePort(addr string) ([]uint32, error) {
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	req := AskFreePortReq{}
	raw, _ := json.Marshal(&req)
	err = sendMessage(conn, TypeAskFreePortReq, raw)
	if err != nil {
		return nil, err
	}
	mtype, data, err := recvMessage(conn)
	if err != nil {
		return nil, err
	}
	if mtype != TypeAskFreePortRsp {
		return nil, errors.New("protocol mismatch")
	}

	rsp := AskFreePortRsp{}
	err = json.Unmarshal(data, &rsp)
	if err != nil {
		return nil, err
	}

	sort.Slice(rsp.Ports, func(i, j int) bool {
		return rsp.Ports[i] < rsp.Ports[j]
	})
	return rsp.Ports, nil
}

// DiscoverServices Discover all active services from proxy server with given address.
func DiscoverServices(addr string) ([]ServiceInfo, error) {
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	req := AskServiceReq{}
	raw, _ := json.Marshal(&req)
	err = sendMessage(conn, TypeAskServiceReq, raw)
	if err != nil {
		return nil, err
	}
	mtype, data, err := recvMessage(conn)
	if err != nil {
		return nil, err
	}
	if mtype != TypeAskServiceRsp {
		return nil, errors.New("protocol mismatch")
	}

	rsp := AskServiceRsp{}
	err = json.Unmarshal(data, &rsp)
	if err != nil {
		return nil, err
	}

	return rsp.Services, nil
}
