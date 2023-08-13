/*
Package proxylite is a dynamic reverse proxy package for Go.
*/
package proxylite

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
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
	defer func() {
		if err := recover(); err != nil {
			s.logTunnelMessage(tn.info.Name, "PANIC", fmt.Sprint("tunnel panic, err=", err))
		}

		(*tn.innerConn).Close()
		s.logTunnelMessage(tn.info.Name, "CLOSE", fmt.Sprint("tunnel closed, port=", tn.info.OuterPort))
		
		tn.empty = true
		fmt.Println("set empty ", tn.empty)
		fmt.Println(s.used[tn.info.OuterPort])
	}()
	
	// Now, register is OK, we want to map outer port to the inner client's socket
	s.logTunnelMessage(tn.info.Name, "REGISTER", fmt.Sprintf("New inner client register port %d ok. Listening for outer client...", tn.info.OuterPort))

	// First, listen on registered outer port.
	listener, err := net.Listen("tcp", fmt.Sprintf("0.0.0.0:%d", tn.info.OuterPort))
	if err != nil {
		panic(err)
	}
	// listener must be closed when leave this method
	defer listener.Close()

	// outerConn is the connection between our outer port and user.
	var outerConn net.Conn

	// outerAccept is a signaling channel. 
	outerAccept := make(chan struct{})
	go func() {
		// must close outerAccept when leaving this goroutine
		defer close(outerAccept)
		outerConn, err = listener.Accept()
		if err != nil {
			// TODO: We think error caused by listener being closed is normal. But what about other errors?
			return
		}
		// here means we succeed to accept the connection
		outerAccept <- struct{}{}
	}()
	
	// innerData is a temporary buffer channel to store the data send from inner client BEFORE any user connect to our outer port. We need to forward these data to user after the connection is readable.
	innerData := make(chan []byte, 64)

	// innerEOF and innerOK is 2 possible exit state of below goroutine. We use them to decide whether abort this Tunnel.
	innerEOF := make(chan struct{})
	defer close(innerEOF)
	innerOK := make(chan struct{})
	defer close(innerOK)
	go func() {
		for {
			// in order to prevent the inner client connect from blocking reading, we set 1s deadline.
			(*tn.innerConn).SetReadDeadline(time.Now().Add(time.Second))
			buf := make([]byte, 4094)
			n, err := (*tn.innerConn).Read(buf)
			if err == io.EOF {
				// io.EOF means inner client active disconnect. We signal and close the EOF channel. Then return.
				innerEOF <- struct{}{}
				close(innerData)
				return
			}
			// TODO: we ignore the error caused by Deadline timeout. But what about other errors?  

			if err == nil && n > 0 {
				// Here mean we do read some data from inner client. Now user haven't come, so we store the data for him.
				select {
				case innerData <- buf[:n]:
				// if innerData is already full, the select block will go to default.
				default:
					// we panic this condition. Don't worry this panic will be recovered.
					panic(errors.New("buffer overflow"))
				}
			}
			
			// Then we check whether we get the user connection.
			select {
			case <-outerAccept:
				// Now we already get the user connection. We will signal innerOK. Before that, we close the innerData buffer.
				// The sequence of below 2 lines are important. MAKE SURE innerData is closed when we for-range the data in it.
				close(innerData)
				innerOK <- struct{}{}
				return
			default:
				// Nothing happend. We continue the loop.
			}
		}
	}()
	
	// Now the judge the 2 possible exit state.
	select {
	case <-innerOK:
		// Now we get the connnect from user and already stored all data from innner client and we have closed the innerData chan.
		// Cancel the deadline
		(*tn.innerConn).SetReadDeadline(time.Time{})
	case <-innerEOF:
		// Now inner client close the connect. We just return and listener will be closed in defer func.
		return
	}

	// Don't forget to close the user connection
	defer outerConn.Close()

	// Now real communication begins. We set tunnel busy.
	tn.busy = true

	// Move each peace of buffered data to user conn.
	for data := range innerData {
		n, err := outerConn.Write(data)
		if err != nil {
			return
		}
		s.logTunnelMessage(tn.info.Name, "BUFMOVE", fmt.Sprintf("move %dB data from buffer to outer conn", n))
	}

	s.logTunnelMessage(tn.info.Name, "CONNECT", fmt.Sprintf("accpet connection from: %s", outerConn.RemoteAddr().String()))
	
	// anyClose is a signal channel to wait any of 2 below goroutine exit.
	anyClose := make(chan struct{})

	go func() {
		// read from user conn and write to inner client
		total, err := io.Copy(*tn.innerConn, outerConn)
		s.logTunnelMessage(tn.info.Name, "FINISH", fmt.Sprintf("out->in finish: %v, [%d], [%v]", outerConn.RemoteAddr(), total, err))
		<-anyClose
	}()

	go func() {
		// read from inner conn and write to user conn
		total, err := io.Copy(outerConn, *tn.innerConn)
		s.logTunnelMessage(tn.info.Name, "FINISH", fmt.Sprintf("in->out finish: %v, [%d], [%v]", outerConn.RemoteAddr(), total, err))
		<-anyClose
	}()
	
	// write anyClose will block here until any of 2 above gouroutine done. Then we close it.
	anyClose <- struct{}{}
	close(anyClose)

	// Drain out the sending buffer. The Tunnel can be destroy now.
	time.Sleep(time.Millisecond * 10)
}

func (s *ProxyLiteServer) logProtocolMessage(source, header string, req, rsp interface{}) {
	s.logger.Infof("[%s] [%s] req=%#v, rsp=%#v", source, header, req, rsp)
}

func (s *ProxyLiteServer) logTunnelMessage(service, header, msg string) {
	s.logger.Infof("[%s] [%s] %s", service, header, msg)
}
