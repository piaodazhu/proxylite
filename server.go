/*
Package proxylite is a dynamic reverse proxy package for Go.
*/
package proxylite

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"time"

	log "github.com/sirupsen/logrus"
	sem "golang.org/x/sync/semaphore"
)

type tunnel struct {
	empty bool
	// busy  bool
	// birth     time.Time
	innerConn *net.Conn
	// info      *RegisterInfo
	service *ServiceInfo
	ctrl    *ControlInfo
}

// ProxyLiteServer Public server that forwards traffic between user and inner client.
type ProxyLiteServer struct {
	all    map[uint32]struct{}
	lock   sync.RWMutex
	used   map[uint32]*tunnel
	logger *log.Logger
}

// NewProxyLiteServer Create a Proxy server with available ports intervals.
func NewProxyLiteServer(portIntervals ...[2]int) *ProxyLiteServer {
	server := &ProxyLiteServer{
		all:    map[uint32]struct{}{},
		used:   map[uint32]*tunnel{},
		logger: log.New(),
	}
	// server.logger.SetReportCaller(true)
	for _, intervals := range portIntervals {
		server.AddPort(intervals[0], intervals[1])
	}
	return server
}

// AddPort Add available ports intervals for server. Return false if port is invalid.
func (s *ProxyLiteServer) AddPort(from, to int) bool {
	if from <= 0 || to > 65535 {
		return false
	}
	for p := from; p <= to; p++ {
		s.all[uint32(p)] = struct{}{}
	}
	return true
}

// SetLogger Set customized logrus logger for the server.
func (s *ProxyLiteServer) SetLogger(logger *log.Logger) {
	s.logger = logger
}

// Run Run the server and let it listen on given address.
func (s *ProxyLiteServer) Run(addr string) error {
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	s.logger.Info("Listening for new inner client, addr=", addr)
	for {
		conn, err := listener.Accept()
		if err != nil {
			s.logger.Error(err)
			continue
		}

		s.logger.Info("Accept new client: ", conn.RemoteAddr())
		err = s.serve(conn)
		if err != nil {
			s.logger.Error(err)
		}
	}
}

func (s *ProxyLiteServer) serve(conn net.Conn) error {
	mtype, buf, err := recvMessage(conn)
	if err != nil {
		return err
	}
	switch mtype {
	case TypeAskFreePortReq:
		req := AskFreePortReq{}
		err := json.Unmarshal(buf, &req)
		if err != nil {
			return s.handleMsgMalformed(conn)
		}
		return s.handleAskFreePort(conn, &req)
	case TypeAskServiceReq:
		req := AskServiceReq{}
		err := json.Unmarshal(buf, &req)
		if err != nil {
			return s.handleMsgMalformed(conn)
		}
		return s.handleAskService(conn, &req)
	case TypeRegisterServiceReq:
		req := RegisterServiceReq{}
		err := json.Unmarshal(buf, &req)
		if err != nil {
			return s.handleMsgMalformed(conn)
		}
		return s.handleRegisterService(conn, &req)
	default:
		return s.handleMsgUndefined(conn)
	}
}

func (s *ProxyLiteServer) handleAskFreePort(conn net.Conn, req *AskFreePortReq) error {
	defer conn.Close()
	freePorts := []uint32{}

	s.lock.RLock()
	for port := range s.all {
		if tunnel, ok := s.used[port]; !ok {
			freePorts = append(freePorts, port)
		} else if tunnel.empty {
			freePorts = append(freePorts, port)
		}
	}
	s.lock.RUnlock()

	rsp := AskFreePortRsp{
		Ports: freePorts,
	}
	defer s.logProtocolMessage(conn.RemoteAddr().String(), "AskFreePort", req, &rsp)

	raw, _ := json.Marshal(rsp)
	err := sendMessage(conn, TypeAskFreePortRsp, raw)
	if err != nil {
		return err
	}
	return nil
}

func (s *ProxyLiteServer) handleAskService(conn net.Conn, req *AskServiceReq) error {
	defer conn.Close()
	Services := []ServiceInfo{}
	s.lock.RLock()

	for _, t := range s.used {
		if !t.empty &&
			len(t.service.Name) >= len(req.Prefix) &&
			t.service.Name[:len(req.Prefix)] == req.Prefix {
			Services = append(Services, *t.service)
		}
	}
	s.lock.RUnlock()
	rsp := AskServiceRsp{
		Services: Services,
	}
	defer s.logProtocolMessage(conn.RemoteAddr().String(), "AskService", req, &rsp)

	raw, _ := json.Marshal(rsp)
	err := sendMessage(conn, TypeAskServiceRsp, raw)
	if err != nil {
		return err
	}
	return nil
}

func (s *ProxyLiteServer) handleRegisterService(conn net.Conn, req *RegisterServiceReq) error {
	if _, allow := s.all[req.Info.OuterPort]; !allow {
		return s.registerServiceResponse(conn, req, RegisterRspPortNotAllowed)
	}

	s.lock.Lock()
	defer s.lock.Unlock()
	tn, found := s.used[req.Info.OuterPort]
	if !found {
		tn = &tunnel{
			empty:     false,
			innerConn: &conn,
			ctrl:      &req.Ctrl,
		}
		s.used[req.Info.OuterPort] = tn
	} else if tn.empty { // reuse
		tn.empty = false
		tn.innerConn = &conn
		tn.ctrl = &req.Ctrl
	} else {
		return s.registerServiceResponse(conn, req, RegisterRspPortOccupied)
	}

	tn.service = &ServiceInfo{
		Port:    req.Info.OuterPort,
		Name:    req.Info.Name,
		Message: req.Info.Message,
		Birth:   time.Now(),
	}

	if req.Ctrl.MaxServeConn != 0 {
		tn.service.Capacity = req.Ctrl.MaxServeConn
	} else {
		tn.service.Capacity = 10000
	}

	if req.Ctrl.MaxServeCount != 0 {
		tn.service.TotalServe = req.Ctrl.MaxServeCount
	} else {
		tn.service.TotalServe = 100000
	}

	if req.Ctrl.MaxServeTime != 0 {
		tn.service.DeadLine = time.Now().Add(time.Second * time.Duration(req.Ctrl.MaxServeTime))
	} else {
		tn.service.DeadLine = time.Now().Add(time.Hour * 24 * 7 * 365)
	}

	s.registerServiceResponse(conn, req, RegisterRspOK)
	go s.startTunnel(tn)
	return nil
}

func (s *ProxyLiteServer) handleMsgUndefined(conn net.Conn) error {
	defer s.logProtocolMessage(conn.RemoteAddr().String(), "MsgUndefined", nil, nil)
	sendMessage(conn, TypeMsgUndefined, nil)
	conn.Close()
	return fmt.Errorf("msg is undefined")
}

func (s *ProxyLiteServer) handleMsgMalformed(conn net.Conn) error {
	defer s.logProtocolMessage(conn.RemoteAddr().String(), "MsgMalformed", nil, nil)
	sendMessage(conn, TypeMsgMalformed, nil)
	conn.Close()
	return fmt.Errorf("msg is malformed")
}

func (s *ProxyLiteServer) registerServiceResponse(conn net.Conn, req *RegisterServiceReq, code int32) error {
	if code != RegisterRspOK {
		defer conn.Close()
	}

	rsp := RegisterServiceRsp{
		Code: code,
	}
	defer s.logProtocolMessage(conn.RemoteAddr().String(), "RegisterService", req, &rsp)
	raw, _ := json.Marshal(rsp)
	err := sendMessage(conn, TypeRegisterServiceRsp, raw)
	if err != nil {
		return err
	}
	return nil
}

// startTunnel The core of server. Very confusing code...
func (s *ProxyLiteServer) startTunnel(tn *tunnel) {
	doOnce := sync.Once{}
	binder := newConnUidBinder(0)
	var totalOut, totalIn uint64

	// Now, register is OK, we want to map outer port to the inner client's socket
	s.logTunnelMessage(tn.service.Name, "REGISTER", fmt.Sprintf("New inner client register port %d ok. Listening for outer client...", tn.service.Port))

	// First, listen on registered outer port.
	listener, err := net.Listen("tcp", fmt.Sprintf("0.0.0.0:%d", tn.service.Port))
	if err != nil {
		(*tn.innerConn).Close()
		return
	}

	// close both "proxy server <-> inner client" and "outer port listener" and all user connection
	closeTunnel := func() {
		(*tn.innerConn).Close()
		listener.Close()
		binder.closeAll()
		s.logTunnelMessage(tn.service.Name, "CLOSE", fmt.Sprintf("tunnel closed, port=%d, Out %dB, In %dB", tn.service.Port, totalOut, totalIn))
	}

	// if MaxServeTime is set, close all conn after MaxServeTime s
	var timeoutTimer *time.Timer
	if tn.ctrl.MaxServeTime > 0 {
		timeoutTimer = time.AfterFunc(time.Second*time.Duration(tn.ctrl.MaxServeTime), func() {
			doOnce.Do(closeTunnel)
		})
	}

	// if MaxServeConn is set, use semaphore for concurrency control
	var concurrency *sem.Weighted
	if tn.ctrl.MaxServeConn > 0 {
		concurrency = sem.NewWeighted(int64(tn.ctrl.MaxServeConn))
	} else {
		concurrency = sem.NewWeighted(10000) // only for max 10K user
	}

	// if MaxServeCount is set not zero, only serve MaxServeCount users.
	var coming, leaving *sem.Weighted
	var finiteServeCount bool = false
	lastFinish := sync.WaitGroup{}
	if tn.ctrl.MaxServeCount > 0 {
		finiteServeCount = true
		coming = sem.NewWeighted(int64(tn.ctrl.MaxServeCount))
		leaving = sem.NewWeighted(int64(tn.ctrl.MaxServeCount) - 1) // last one leave, close tunnel
		lastFinish.Add(1)
	}

	// make sure tunnel closed and tn set empty before this function return
	defer func() {
		if err := recover(); err != nil {
			s.logTunnelMessage(tn.service.Name, "PANIC", fmt.Sprint("tunnel panic, err=", err))
		}
		if timeoutTimer != nil && !timeoutTimer.Stop() {
			select { // timer may not be triggered. If so we drain it out.
			case <-timeoutTimer.C:
			default:
			}
		}
		doOnce.Do(closeTunnel)
		tn.empty = true
	}()

	// read from tunnel and send to correct user
	go func() {
		buf := make([]byte, 32768)
		var mtype, n int
		var data []byte
		var err error
		var outerConn *net.Conn
		var ok, alive bool
		var uid uint32
		for {
			// read from inner client
			mtype, data, err = recvMessageWithBuffer(*tn.innerConn, buf)
			if err != nil || mtype != TypeDataSegment {
				break
			}
			// get multiplex uid
			uid, alive = readUidUnsafe(data)
			if outerConn, ok = binder.getConn(uid); !ok {
				// unexpected uid, drop the pack
				// TODO send a close signal
				continue
			} else if !alive {
				if binder.freeUidIfExists(GenConnId(outerConn)) {
					(*outerConn).Close()
				}
			}

			// forward to the write user. (don't send 4 byte uid)
			// here can be optimized by goroutines
			n, err = (*outerConn).Write(data[8:])
			if err != nil {
				// how to send close thougth tunnel?
				// TODO send a close signal
				if binder.freeUidIfExists(GenConnId(outerConn)) {
					(*outerConn).Close()
				}
			}
			totalOut += uint64(n)
		}
		doOnce.Do(closeTunnel)
	}()

	// read from user and send to tunnel
	readFromUser := func(outerConn net.Conn, outerConnId uint64, uid uint32) {
		buf := make([]byte, 32768)
		var n int
		var err error
		var userEnd bool = false

		for !userEnd {
			n, err = outerConn.Read(buf[16:]) // type(4) + length(4) + uid(4) + close(4)
			if err != nil {
				// end this link
				writeUidWithCloseUnsafe(buf[8:], uid)
				userEnd = true
			} else {
				writeUidUnsafe(buf[8:], uid)
			}

			err = sendMessageOnBuffer(*tn.innerConn, TypeDataSegment, buf, n+8) // + uid(4) + close(4)
			if err != nil {
				// end this tunnel
				doOnce.Do(closeTunnel)
				break
			}
		}
		if binder.freeUidIfExists(GenConnId(&outerConn)) {
			outerConn.Close()
			s.logTunnelMessage(tn.service.Name, "FINISH", fmt.Sprintf("in->out finish: %v, [%v]", outerConn.RemoteAddr(), err))
		}
		concurrency.Release(1)
		// if MaxServeCount is defined and the last one user leave
		if finiteServeCount && !leaving.TryAcquire(1) {
			doOnce.Do(closeTunnel)
			lastFinish.Done() // can finish
		}
		atomic.AddUint32(&tn.service.Online, ^uint32(0)) // sub 1
	}

	// accept new user and alloc uid and start send loop
	for {
		// concurrent control
		if !concurrency.TryAcquire(1) {
			// if online user count reach maxConn, step into this branch
			// we close listener to 'reject' unexpected users
			listener.Close()
			// Then block until at lease one user finish his session.
			concurrency.Acquire(context.Background(), 1)
			// Then we recover the listener
			listener, err = net.Listen("tcp", fmt.Sprintf("0.0.0.0:%d", tn.service.Port))
			if err != nil {
				break
			}
		}
		if finiteServeCount {
			// All users have been come.
			if !coming.TryAcquire(1) {
				lastFinish.Wait() // wait last user finish
				break
			}
		}
		conn, err := listener.Accept()
		if err != nil {
			break
		}
		connId := GenConnId(&conn)
		if uid, ok := binder.allocUid(connId, &conn); ok {
			atomic.AddUint32(&tn.service.AlreadyServe, 1)
			atomic.AddUint32(&tn.service.Online, 1)
			go readFromUser(conn, connId, uid)
		} else {
			conn.Close()
		}
	}
	doOnce.Do(closeTunnel)
}

func (s *ProxyLiteServer) logProtocolMessage(source, header string, req, rsp interface{}) {
	s.logger.Infof("[%s] [%s] req=%#v, rsp=%#v", source, header, req, rsp)
}

func (s *ProxyLiteServer) logTunnelMessage(service, header, msg string) {
	s.logger.Infof("[%s] [%s] %s", service, header, msg)
}
