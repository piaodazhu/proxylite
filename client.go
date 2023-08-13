package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"sort"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"
)

type RegisterEntry struct {
	Info   RegisterInfo
	Cancel func()
	Done   <-chan struct{}
}

type ProxyLiteClient struct {
	ready          bool
	serverAddr     string
	lock           sync.RWMutex
	avaliablePorts map[int]struct{}
	registered     map[int]*RegisterEntry
	logger         *log.Logger
}

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
	innerConn, err := net.Dial("tcp", info.InnerAddr)
	if err != nil {
		return err
	}

	// If we cannot connect to proxy server. We cannot register any service. And set client not ready.
	serverConn, err := net.Dial("tcp", c.serverAddr)
	if err != nil {
		c.ready = false
		innerConn.Close()
		return err
	}

	// Register the service must return nil.
	err = register(serverConn, info)
	c.logTunnelMessage(info.Name, "REGISTER", fmt.Sprint("err=", err))
	if err != nil {
		innerConn.Close()
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

	go func() {
		// anyClose is a signal channel to wait any of 2 below goroutine exit.
		anyClose := make(chan struct{})

		go func() {
			total, err := io.Copy(innerConn, serverConn)
			c.logTunnelMessage(info.Name, "FINISH", fmt.Sprintf("server->inner Finish [%d], [%v]", total, err))
			<-anyClose
		}()

		go func() {
			total, err := io.Copy(serverConn, innerConn)
			c.logTunnelMessage(info.Name, "FINISH", fmt.Sprintf("inner->server Finish [%d], [%v]", total, err))
			<-anyClose
		}()

		select {
		case anyClose <- struct{}{}:
			// Any of 2 above gouroutine done.
		case <-cancel:
			// Here we receive cancel signal by caller.
			c.logTunnelMessage(info.Name, "CANCEL", "service cancelled")
		}

		// Good principle: let writer goroutine to close the channel.
		close(anyClose)

		// Drain out the sending buffer. The Tunnel can be destroy now.
		time.Sleep(time.Millisecond * 10)
		innerConn.Close()
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

func (c *ProxyLiteClient) GetRegisterEntryByPort(port int) (*RegisterEntry, bool) {
	c.lock.RLock()
	defer c.lock.RUnlock()
	entry, exists := c.registered[port]
	return entry, exists
}

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

func (c *ProxyLiteClient) SetLogger(logger *log.Logger) {
	c.logger = logger
}

func (c *ProxyLiteClient) logTunnelMessage(service, header, msg string) {
	c.logger.Infof("[%s] [%s] %s", service, header, msg)
}
