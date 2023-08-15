/*
Package proxylite is a dynamic reverse proxy package for Go.
*/
package proxylite

import (
	"encoding/json"
	"fmt"
	"net"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"
)

type tunnel struct {
	empty     bool
	busy      bool
	birth     time.Time
	innerConn *net.Conn
	info      *RegisterInfo
}

// ProxyLiteServer Public server that forwards traffic between user and inner client.
type ProxyLiteServer struct {
	all    map[int]struct{}
	lock   sync.RWMutex
	used   map[int]*tunnel
	logger *log.Logger
}

// NewProxyLiteServer Create a Proxy server with avaliable ports intervals.
func NewProxyLiteServer(portIntervals ...[2]int) *ProxyLiteServer {
	server := &ProxyLiteServer{
		all:    map[int]struct{}{},
		used:   map[int]*tunnel{},
		logger: log.New(),
	}
	server.logger.SetReportCaller(true)
	for _, intervals := range portIntervals {
		server.AddPort(intervals[0], intervals[1])
	}
	return server
}

// AddPort Add avaliable ports intervals for server. Return false if port is invalid.
func (s *ProxyLiteServer) AddPort(from, to int) bool {
	if from <= 0 || to > 65535 {
		return false
	}
	for p := from; p <= to; p++ {
		s.all[p] = struct{}{}
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
	freePorts := []int{}

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
	fmt.Println("#", freePorts)
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
	fmt.Println("* ", s.used[9931])
	s.lock.RLock()

	for _, t := range s.used {
		if !t.empty &&
			len(t.info.Name) >= len(req.Prefix) &&
			t.info.Name[:len(req.Prefix)] == req.Prefix {
			Services = append(Services, ServiceInfo{
				Port:    t.info.OuterPort,
				Name:    t.info.Name,
				Message: t.info.Message,
				Busy:    t.busy,
				Birth:   t.birth,
			})
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
			busy:      false,
			birth:     time.Now(),
			innerConn: &conn,
			info:      &req.Info,
		}
		s.used[req.Info.OuterPort] = tn
	} else if tn.empty { // reuse
		tn.empty = false
		tn.busy = false
		tn.birth = time.Now()
		tn.innerConn = &conn
		tn.info = &req.Info
	} else {
		return s.registerServiceResponse(conn, req, RegisterRspPortOccupied)
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

func (s *ProxyLiteServer) registerServiceResponse(conn net.Conn, req *RegisterServiceReq, code int) error {
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
	binder := newConnUidBinder(2)
	var totalOut, totalIn uint64

	// Now, register is OK, we want to map outer port to the inner client's socket
	s.logTunnelMessage(tn.info.Name, "REGISTER", fmt.Sprintf("New inner client register port %d ok. Listening for outer client...", tn.info.OuterPort))

	// First, listen on registered outer port.
	listener, err := net.Listen("tcp", fmt.Sprintf("0.0.0.0:%d", tn.info.OuterPort))
	if err != nil {
		(*tn.innerConn).Close()
		return
	}

	// close both "proxy server <-> inner client" and "outer port listener" and all user connection
	closeTunnel := func() {
		(*tn.innerConn).Close()
		listener.Close()
		binder.closeAll()
		s.logTunnelMessage(tn.info.Name, "CLOSE", fmt.Sprintf("tunnel closed, port=%d, Out %dB, In %dB", tn.info.OuterPort, totalOut, totalIn))
	}

	// make sure tunnel closed and tn set empty before this function return
	defer func() {
		if err := recover(); err != nil {
			s.logTunnelMessage(tn.info.Name, "PANIC", fmt.Sprint("tunnel panic, err=", err))
		}
		doOnce.Do(closeTunnel)
		tn.empty = true
		fmt.Println("set empty ", tn.empty)
		fmt.Println(s.used[tn.info.OuterPort])
	}()

	// read from tunnel and send to correct user
	go func() {
		buf := make([]byte, 4096)
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
		buf := make([]byte, 4096)
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

			err = sendMessageOnBuffer(*tn.innerConn, TypeDataSegment, buf, n+8) // + uid(4)
			if err != nil {
				// end this tunnel
				doOnce.Do(closeTunnel)
				break
			}
			totalIn += uint64(n)
		}
		if binder.freeUidIfExists(GenConnId(&outerConn)) {
			outerConn.Close()
			s.logTunnelMessage(tn.info.Name, "FINISH", fmt.Sprintf("in->out finish: %v, [%v]", outerConn.RemoteAddr(), err))
		}
	}

	// accept new user and alloc uid and start send loop
	for {
		conn, err := listener.Accept()
		if err != nil {
			break
		}
		connId := GenConnId(&conn)
		if uid, ok := binder.allocUid(connId, &conn); ok {
			go readFromUser(conn, connId, uid)
		} else {
			conn.Close()
		}
	}
	doOnce.Do(closeTunnel)

	// Drain out the sending buffer. The Tunnel can be destroy now.
	time.Sleep(time.Millisecond * 10)
}

func (s *ProxyLiteServer) logProtocolMessage(source, header string, req, rsp interface{}) {
	s.logger.Infof("[%s] [%s] req=%#v, rsp=%#v", source, header, req, rsp)
}

func (s *ProxyLiteServer) logTunnelMessage(service, header, msg string) {
	s.logger.Infof("[%s] [%s] %s", service, header, msg)
}
