package proxylite

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"sort"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"
)

// RegisterEntry entry to discribe a single service registration
type RegisterEntry struct {
	// Basic Info
	Info RegisterInfo
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
	avaliablePorts map[int]struct{}
	registered     map[int]*RegisterEntry
	logger         *log.Logger
}

// NewProxyLiteClient Create a inner client binding with a proxy server.
func NewProxyLiteClient(serverAddr string) *ProxyLiteClient {
	client := &ProxyLiteClient{
		ready:          false,
		serverAddr:     serverAddr,
		avaliablePorts: map[int]struct{}{},
		registered:     map[int]*RegisterEntry{},
		logger:         log.New(),
	}
	client.logger.SetReportCaller(true)

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
func (c *ProxyLiteClient) AvaliablePorts() ([]int, bool) {
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
func (c *ProxyLiteClient) AnyPort() (int, bool) {
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

func register(conn net.Conn, info RegisterInfo) error {
	req := RegisterServiceReq{
		Info: RegisterInfo{
			OuterPort: info.OuterPort,
			InnerAddr: info.InnerAddr,
		},
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
func (c *ProxyLiteClient) RegisterInnerService(info RegisterInfo) error {
	if !c.ready {
		return errors.New("client not ready")
	}
	c.lock.Lock()
	defer c.lock.Unlock()
	if _, exists := c.registered[info.OuterPort]; exists {
		return errors.New("ports is occupied")
	}

	// If we cannot connect to inner target service. We should not register this service to proxy server.
	// innerConn, err := net.Dial("tcp", info.InnerAddr)
	// if err != nil {
	// 	return err
	// }

	// If we cannot connect to proxy server. We cannot register any service. And set client not ready.
	serverConn, err := net.Dial("tcp", c.serverAddr)
	if err != nil {
		c.ready = false
		// innerConn.Close()
		return err
	}

	// Register the service must return nil.
	err = register(serverConn, info)
	c.logTunnelMessage(info.Name, "REGISTER", fmt.Sprint("err=", err))
	if err != nil {
		// innerConn.Close()
		serverConn.Close()
		return err
	}

	// cancelFunc can be used to cancel the service mapping
	cancel := make(chan struct{})
	cancelFunc := func() {
		cancel <- struct{}{}
		close(cancel)
	}

	// done channel can be used to wait the service mapping finish
	done := make(chan struct{})

	// Now we can add the new entry to registered table.
	c.registered[info.OuterPort] = &RegisterEntry{
		Info:   info,
		Cancel: cancelFunc,
		Done:   done,
	}

	// // 1 -> N
	// go func() {
	// 	buf := make([]byte, 4096)
	// 	var mtype, n int
	// 	var total uint64
	// 	var data []byte
	// 	for {
	// 		mtype, data, err = recvMessageWithBuffer(serverConn, buf)
	// 		if err != nil || mtype != TypeDataSegment {
	// 			break
	// 		}

	// 		n, err = innerConn.Write(data)
	// 		if err != nil {
	// 			break
	// 		}

	// 		total += uint64(n)
	// 	}
	// }()

	binder := newConnUidBinder(0)

	recvLoop := func(inner net.Conn, uid uint32) {
		buf := make([]byte, 4096)
		var n int
		var total uint64
		for {
			n, err = inner.Read(buf[12:]) // type(4) + length(4) + uid(4)
			if err != nil {
				break
			}

			writeUidUnsafe(buf[8:], uid)

			err = sendMessageOnBuffer(serverConn, TypeDataSegment, buf, n+4) // + uid(4)
			if err != nil {
				break
			}

			total += uint64(n)
		}
		// <-anyClose

		// tell him loop is done!

		// TODO A: A and B just do one
		inner.Close()
		binder.freeConnIfExists(uid)
	}

	go func() {
		// anyClose is a signal channel to wait any of 2 below goroutine exit.
		// anyClose := make(chan struct{})

		go func() {
			// total, err := io.Copy(innerConn, serverConn)
			// c.logTunnelMessage(info.Name, "FINISH", fmt.Sprintf("server->inner Finish [%d], [%v]", total, err))

			buf := make([]byte, 4096)
			var mtype, n int
			var total uint64
			var data []byte

			var uid uint32
			var innerConn *net.Conn
			var ok bool

			for {
				// TODO set deadline and allow cancel
				mtype, data, err = recvMessageWithBuffer(serverConn, buf)
				if err != nil || mtype != TypeDataSegment {
					break
				}

				uid = readUidUnsafe(data)
				if innerConn, ok = binder.getConn(uid); !ok {
					c, err := net.Dial("tcp", info.InnerAddr)
					if err != nil {
						break
					}
					innerConn = &c
					if !binder.allocConn(uid, GenConnId(innerConn), innerConn) {
						break
					}

					go recvLoop(c, uid)
				}

				n, err = (*innerConn).Write(data)
				if err != nil {
					// break
					// not break just for one client. but tell him the conn done!
					
					// TODO B: A and B just do one
					(*innerConn).Close()
					binder.freeConnIfExists(uid)
				}

				total += uint64(n)
			}

			binder.freeConnIfExists(uid)
			// <-anyClose
		}()

		// go func() {
		// 	// total, err := io.Copy(serverConn, innerConn)
		// 	// c.logTunnelMessage(info.Name, "FINISH", fmt.Sprintf("inner->server Finish [%d], [%v]", total, err))
		// 	buf := make([]byte, 4096)
		// 	var n int
		// 	var total uint64
		// 	for {
		// 		n, err = innerConn.Read(buf[8:])
		// 		if err != nil {
		// 			break
		// 		}

		// 		err = sendMessageOnBuffer(serverConn, TypeDataSegment, buf, n)
		// 		if err != nil {
		// 			break
		// 		}

		// 		total += uint64(n)
		// 	}
		// 	<-anyClose
		// }()

		// select {
		// case anyClose <- struct{}{}:
			// Any of 2 above gouroutine done.
		// case <-cancel:
			// Here we receive cancel signal by caller.
			// c.logTunnelMessage(info.Name, "CANCEL", "service cancelled")
		// }

		// Good principle: let writer goroutine to close the channel.
		// close(anyClose)

		// Drain out the sending buffer. The Tunnel can be destroy now.
		time.Sleep(time.Millisecond * 10)
		// innerConn.Close()
		serverConn.Close()

		// Tell caller that the service mapping is already finish.
		done <- struct{}{}
		close(done)

		c.logTunnelMessage(info.Name, "UNREGISTER", "service unregister")

		// Delete the service mapping from table.
		c.lock.Lock()
		delete(c.registered, info.OuterPort)
		c.lock.Unlock()
	}()
	return nil
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
	entry, exists := c.registered[port]
	return entry, exists
}

func (c *ProxyLiteClient) logTunnelMessage(service, header, msg string) {
	c.logger.Infof("[%s] [%s] %s", service, header, msg)
}

// AskFreePort Ask avaliable free port from proxy server with given address.
func AskFreePort(addr string) ([]int, error) {
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

	sort.Ints(rsp.Ports)
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
