package proxylite

import (
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
)

func readn(c net.Conn, n int) ([]byte, error) {
	offset := 0
	buf := make([]byte, n)
	for offset < n {
		n, err := c.Read(buf[offset:])
		if err != nil {
			return nil, err
		}
		offset += n
	}
	return buf, nil
}

func written(c net.Conn, data []byte, n int) error {
	offset := 0

	for offset < n {
		n, err := c.Write(data[offset:n])
		if err != nil {
			return err
		}
		offset += n
	}
	return nil
}

func newTcpEchoServer(addr string, pken int) error {
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	echo := func(c net.Conn) {
		var data []byte
		var err error
		for {
			data, err = readn(c, pken)
			if err != nil {
				break
			}
			err = written(c, data, pken)
			if err != nil {
				break
			}
		}
		c.Close()
	}
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				panic(err)
			}
			go echo(conn)
		}
	}()
	return nil
}

func init() {
	if err := newTcpEchoServer(":9966", 8); err != nil {
		panic(err)
	}
}

func TestBasicUsage(t *testing.T) {
	logger := logrus.New()
	logger.Level = logrus.FatalLevel

	proxyServer := NewProxyLiteServer([2]int{9900, 9910})
	proxyServer.SetLogger(logger)
	proxyServer.AddPort(9968, 9968)
	if proxyServer.AddPort(99968, 99968) == true {
		t.Error("AddPort")
	}
	go func() {
		if err := proxyServer.Run(":9967"); err != nil {
			panic(err)
		}
	}()
	time.Sleep(time.Millisecond * 10)

	innerClient := NewProxyLiteClient(":9967")
	innerClient.SetLogger(logger)

	ports, ok := innerClient.AvailablePorts()
	if !ok || len(ports) != 12 {
		t.Errorf("AvailablePorts")
	}
	port, ok := innerClient.AnyPort()
	if !ok || port < 9900 || (port > 9910 && port != 9968) {
		t.Errorf("AnyPort")
	}

	cancelFunc, done, err := innerClient.RegisterInnerService(
		RegisterInfo{
			OuterPort: 9968,
			InnerAddr: ":9966",
			Name:      "Echo",
			Message:   "TCP Echo Server",
		},
		ControlInfo{},
	)
	if err != nil {
		panic(err)
	}
	defer func() {
		cancelFunc()
		<-done
		proxyServer.Stop()
	}()
	time.Sleep(time.Millisecond * 10)

	user, err := net.Dial("tcp", ":9968")
	if err != nil {
		t.Fatal(err)
	}
	msg := "hello123"
	var data []byte
	for i := 0; i < 10; i++ {
		err := written(user, []byte(msg), 8)
		if err != nil {
			t.Error("write 1, ", err)
		}
		data, err = readn(user, 8)
		if err != nil {
			t.Error("read 1, ", err)
		}
		if string(data) != msg {
			t.Error("read 3, ", string(data))
		}
	}
	user.Close()
	time.Sleep(time.Millisecond * 100)
	select {
	case <-done:
		t.Error("unexpected quit")
	default:
	}

	services, err := innerClient.ActiveServices()
	if err != nil || len(services) != 1 {
		t.Error("ActiveServices")
	}
}

func TestCancel(t *testing.T) {

	logger := logrus.New()
	logger.Level = logrus.FatalLevel

	proxyServer := NewProxyLiteServer()
	proxyServer.SetLogger(logger)
	proxyServer.AddPort(9968, 9968)
	go func() {
		if err := proxyServer.Run(":9967"); err != nil {
			panic(err)
		}
	}()
	time.Sleep(time.Millisecond * 10)

	innerClient := NewProxyLiteClient(":9967")
	innerClient.SetLogger(logger)
	cancelFunc, done, err := innerClient.RegisterInnerService(
		RegisterInfo{
			OuterPort: 9968,
			InnerAddr: ":9966",
			Name:      "Echo",
			Message:   "TCP Echo Server",
		},
		ControlInfo{},
	)
	if err != nil {
		panic(err)
	}
	defer func() {
		cancelFunc()
		<-done
		proxyServer.Stop()
	}()
	time.Sleep(time.Millisecond * 10)

	user, err := net.Dial("tcp", ":9968")
	if err != nil {
		t.Fatal(err)
	}
	msg := "hello123"
	var data []byte
	for i := 0; i < 10; i++ {
		err := written(user, []byte(msg), 8)
		if err != nil {
			t.Error("write 1, ", err)
		}
		data, err = readn(user, 8)
		if err != nil {
			t.Error("read 1, ", err)
		}
		if string(data) != msg {
			t.Error("read 2, ", string(data))
		}
	}
	cancelFunc()
	time.Sleep(time.Millisecond * 100)
	select {
	case <-done:
	default:
		t.Error("unexpected continue")
	}
	<-done
}

func TestMultiplex(t *testing.T) {

	logger := logrus.New()
	logger.Level = logrus.FatalLevel

	proxyServer := NewProxyLiteServer()
	proxyServer.SetLogger(logger)
	proxyServer.AddPort(9968, 9968)
	go func() {
		if err := proxyServer.Run(":9967"); err != nil {
			panic(err)
		}
	}()
	time.Sleep(time.Millisecond * 10)

	innerClient := NewProxyLiteClient(":9967")
	innerClient.SetLogger(logger)
	cancelFunc, done, err := innerClient.RegisterInnerService(
		RegisterInfo{
			OuterPort: 9968,
			InnerAddr: ":9966",
			Name:      "Echo",
			Message:   "TCP Echo Server",
		},
		ControlInfo{},
	)
	if err != nil {
		panic(err)
	}
	defer func() {
		cancelFunc()
		<-done
		proxyServer.Stop()
	}()
	time.Sleep(time.Millisecond * 10)

	errmsg := make(chan string, 100)
	for ii := 0; ii < 9; ii++ {
		go func(i int) {
			user, err := net.Dial("tcp", ":9968")
			if err != nil {
				errmsg <- err.Error()
			}
			msg := fmt.Sprintf("[%d]hello", i)
			t.Logf("user[%d] is %s\n", i, user.LocalAddr().String())
			var data []byte
			for j := 0; j < 10; j++ {
				err := written(user, []byte(msg), 8)
				if err != nil {
					emsg := fmt.Sprintf("[%d][%d]write 1: %v", i, j, err)
					errmsg <- emsg
				}
				data, err = readn(user, 8)
				if err != nil {
					emsg := fmt.Sprintf("[%d][%d]read 1: %v", i, j, err)
					errmsg <- emsg
				}
				if string(data) != msg {
					emsg := fmt.Sprintf("[%d][%d]read 2: %s != %s", i, j, string(data), msg)
					errmsg <- emsg
				}
			}
			user.Close()
		}(ii)
	}

	time.Sleep(time.Millisecond * 100)
	select {
	case <-done:
		t.Error("unexpected continue")
	default:

	}

	for i := 0; i < len(errmsg); i++ {
		msg := <-errmsg
		t.Error(msg)
	}

}

func TestMultiplexMaxTimeControl(t *testing.T) {

	logger := logrus.New()
	logger.Level = logrus.FatalLevel

	proxyServer := NewProxyLiteServer()
	proxyServer.SetLogger(logger)
	proxyServer.AddPort(9968, 9968)
	go func() {
		if err := proxyServer.Run(":9967"); err != nil {
			panic(err)
		}
	}()
	time.Sleep(time.Millisecond * 10)

	innerClient := NewProxyLiteClient(":9967")
	innerClient.SetLogger(logger)
	cancelFunc, done, err := innerClient.RegisterInnerService(
		RegisterInfo{
			OuterPort: 9968,
			InnerAddr: ":9966",
			Name:      "Echo",
			Message:   "TCP Echo Server",
		},
		ControlInfo{
			MaxServeTime: 1, // 1s to close
		},
	)
	if err != nil {
		panic(err)
	}
	defer func() {
		cancelFunc()
		<-done
		proxyServer.Stop()
	}()
	time.Sleep(time.Millisecond * 10)

	errmsg := make(chan string, 100)
	for ii := 0; ii < 9; ii++ {
		go func(i int) {
			user, err := net.Dial("tcp", ":9968")
			if err != nil {
				errmsg <- err.Error()
			}
			msg := fmt.Sprintf("[%d]hello", i)
			t.Logf("user[%d] is %s\n", i, user.LocalAddr().String())
			var data []byte
			for j := 0; j < 10; j++ {
				err := written(user, []byte(msg), 8)
				if err != nil {
					emsg := fmt.Sprintf("[%d][%d]write 1: %v", i, j, err)
					errmsg <- emsg
				}
				time.Sleep(time.Microsecond * 100)
				data, err = readn(user, 8)
				if err != nil {
					emsg := fmt.Sprintf("[%d][%d]read 1: %v", i, j, err)
					errmsg <- emsg
				}
				time.Sleep(time.Microsecond * 100)
				if string(data) != msg {
					emsg := fmt.Sprintf("[%d][%d]read 2: %s != %s", i, j, string(data), msg)
					errmsg <- emsg
				}
			}
			user.Close()
		}(ii)
	}

	time.Sleep(time.Second * 2)
	select {
	case <-done:
		// must done
	default:
		t.Error("unexpected continue")
	}
}

func TestMultiplexMaxCountControl(t *testing.T) {
	logger := logrus.New()
	logger.Level = logrus.FatalLevel

	proxyServer := NewProxyLiteServer()
	proxyServer.SetLogger(logger)
	proxyServer.AddPort(9968, 9968)
	go func() {
		if err := proxyServer.Run(":9967"); err != nil {
			panic(err)
		}
	}()
	time.Sleep(time.Millisecond * 10)

	innerClient := NewProxyLiteClient(":9967")
	innerClient.SetLogger(logger)
	cancelFunc, done, err := innerClient.RegisterInnerService(
		RegisterInfo{
			OuterPort: 9968,
			InnerAddr: ":9966",
			Name:      "Echo",
			Message:   "TCP Echo Server",
		},
		ControlInfo{
			MaxServeCount: 2,
		},
	)
	if err != nil {
		panic(err)
	}
	defer func() {
		cancelFunc()
		<-done
		proxyServer.Stop()
	}()
	time.Sleep(time.Millisecond * 10)

	errmsg := make(chan string, 100)
	recordmsg := make(chan int, 100)
	for ii := 0; ii < 3; ii++ {
		go func(i int) {
			user, err := net.Dial("tcp", ":9968")
			if err != nil {
				errmsg <- err.Error()
			}
			msg := fmt.Sprintf("[%d]hello", i)
			var data []byte
			for j := 0; j < 10; j++ {
				err := written(user, []byte(msg), 8)
				if err != nil {
					emsg := fmt.Sprintf("[%d][%d]write 1: %v", i, j, err)
					errmsg <- emsg
				}
				data, err = readn(user, 8)
				if err != nil {
					emsg := fmt.Sprintf("[%d][%d]read 1: %v", i, j, err)
					errmsg <- emsg
				}
				if string(data) != msg {
					emsg := fmt.Sprintf("[%d][%d]read 2: %s != %s", i, j, string(data), msg)
					errmsg <- emsg
				} else {
					recordmsg <- i*100 + j + 100
				}
			}
			user.Close()
		}(ii)
	}

	time.Sleep(time.Millisecond * 100)
	select {
	case <-done:
		// must done
	default:
		t.Error("unexpected continue")
	}
	if len(recordmsg) != 20 {
		t.Error("count control error")
	}
}

func TestMultiplexMaxConnControl(t *testing.T) {

	logger := logrus.New()
	logger.Level = logrus.FatalLevel

	proxyServer := NewProxyLiteServer()
	proxyServer.SetLogger(logger)
	proxyServer.AddPort(9968, 9968)
	go func() {
		if err := proxyServer.Run(":9967"); err != nil {
			panic(err)
		}
	}()
	time.Sleep(time.Millisecond * 10)

	innerClient := NewProxyLiteClient(":9967")
	innerClient.SetLogger(logger)
	cancelFunc, done, err := innerClient.RegisterInnerService(
		RegisterInfo{
			OuterPort: 9968,
			InnerAddr: ":9966",
			Name:      "Echo",
			Message:   "TCP Echo Server",
		},
		ControlInfo{
			MaxServeConn: 2,
		},
	)
	if err != nil {
		panic(err)
	}
	defer func() {
		cancelFunc()
		<-done
		proxyServer.Stop()
	}()
	time.Sleep(time.Millisecond * 10)

	errmsg := make(chan string, 100)
	recordmsg := make(chan int, 100)
	for ii := 0; ii < 3; ii++ {
		go func(i int) {
			user, err := net.Dial("tcp", ":9968")
			if err != nil {
				errmsg <- err.Error()
			}
			msg := fmt.Sprintf("[%d]hello", i)
			t.Logf("user[%d] is %s\n", i, user.LocalAddr().String())
			var data []byte
			for j := 0; j < 10; j++ {
				err := written(user, []byte(msg), 8)
				if err != nil {
					emsg := fmt.Sprintf("[%d][%d]write 1: %v", i, j, err)
					errmsg <- emsg
				}
				data, err = readn(user, 8)
				if err != nil {
					emsg := fmt.Sprintf("[%d][%d]read 1: %v", i, j, err)
					errmsg <- emsg
				}
				if string(data) != msg {
					emsg := fmt.Sprintf("[%d][%d]read 2: %s != %s", i, j, string(data), msg)
					errmsg <- emsg
				} else {
					recordmsg <- i*100 + j + 100
				}
			}
			user.Close()
		}(ii)
	}

	time.Sleep(time.Millisecond * 100)
	select {
	case <-done:
		// must done
		t.Error("unexpected quit")
	default:
	}

	if len(recordmsg) != 20 {
		t.Error("count control error")
	}
}

func TestSetHook(t *testing.T) {
	logger := logrus.New()
	logger.Level = logrus.FatalLevel

	proxyServer := NewProxyLiteServer()
	proxyServer.SetLogger(logger)
	proxyServer.AddPort(9968, 9968)

	trace := ""
	proxyServer.OnTunnelCreated(func(ctx *Context) {
		ctx.PutValue("key1", "v1")
		trace += "1"
	})
	proxyServer.OnTunnelDestroyed(func(ctx *Context) {
		if v, ok := ctx.GetValue("key1"); !ok || v.(string) != "v1" {
			panic("kvs doesn't work")
		}
		trace += "2"
	})
	proxyServer.OnUserComming(func(ctx *Context) {
		if v, ok := ctx.GetValue("key1"); !ok || v.(string) != "v1" {
			panic("kvs doesn't work")
		}
		trace += "3"
	})
	proxyServer.OnUserLeaving(func(ctx *Context) {
		if v, ok := ctx.GetValue("key1"); !ok || v.(string) != "v1" {
			panic("kvs doesn't work")
		}
		trace += "4"
	})
	proxyServer.OnForwardTunnelToUser(func(ctx *Context) {
		if v, ok := ctx.GetValue("key1"); !ok || v.(string) != "v1" {
			panic("kvs doesn't work")
		}
		trace += "5"
	})
	proxyServer.OnForwardUserToTunnel(func(ctx *Context) {
		if v, ok := ctx.GetValue("key1"); !ok || v.(string) != "v1" {
			panic("kvs doesn't work")
		}
		trace += "6"
	})

	go func() {
		time.Sleep(time.Millisecond * 10)
		if err := proxyServer.Run(":9967"); err != nil {
			panic(err)
		}
	}()
	time.Sleep(time.Millisecond * 10)

	innerClient := NewProxyLiteClient(":9967")
	innerClient.SetLogger(logger)
	cancelFunc, done, err := innerClient.RegisterInnerService(
		RegisterInfo{
			OuterPort: 9968,
			InnerAddr: ":9966",
			Name:      "Echo",
			Message:   "TCP Echo Server",
		},
		ControlInfo{},
	)
	if err != nil {
		panic(err)
	}
	defer func() {
		cancelFunc()
		<-done
		time.Sleep(time.Millisecond * 10) // wait close
		proxyServer.Stop()
		if trace != "1365656565656565656565642" {
			fmt.Println(trace)
			t.Error("hook does not work well")
		}
	}()
	time.Sleep(time.Millisecond * 10)

	user, err := net.Dial("tcp", ":9968")
	if err != nil {
		t.Fatal(err)
	}
	msg := "hello123"
	var data []byte
	for i := 0; i < 10; i++ {
		err := written(user, []byte(msg), 8)
		if err != nil {
			t.Error("write 1, ", err)
		}
		data, err = readn(user, 8)
		if err != nil {
			t.Error("read 1, ", err)
		}
		if string(data) != msg {
			t.Error("read 3, ", string(data))
		}
	}
	user.Close()
	time.Sleep(time.Millisecond * 100)
	select {
	case <-done:
		t.Error("unexpected quit")
	default:
	}
}

func TestHookContext(t *testing.T) {

	logger := logrus.New()
	logger.Level = logrus.FatalLevel

	proxyServer := NewProxyLiteServer()
	proxyServer.SetLogger(logger)
	proxyServer.AddPort(9968, 9968)

	trace := ""
	proxyServer.OnTunnelCreated(func(ctx *Context) {
		ctx.PutValue("key1", "v1")
		if ctx.DataBuffer() != nil {
			t.Error("data buf not nil")
		}
		sinfo := ctx.ServiceInfo()
		trace += sinfo.Name + sinfo.Message
	})

	proxyServer.OnForwardTunnelToUser(func(ctx *Context) {
		if v, ok := ctx.GetValue("key1"); !ok || v.(string) != "v1" {
			t.Error("getValue")
		}
		uraddr := ctx.UserRemoteAddress().String()
		uladdr := ctx.UserLocalAddress().String()
		ipPort1 := strings.Split(uraddr, ":")
		ipPort2 := strings.Split(uladdr, ":")
		trace += ipPort1[0] + ipPort2[0]

		data := ctx.DataBuffer()
		trace += string(data)
	})
	proxyServer.OnForwardUserToTunnel(func(ctx *Context) {
		if v, ok := ctx.GetValue("key1"); !ok || v.(string) != "v1" {
			panic("kvs doesn't work")
		}
		iraddr := ctx.InnerRemoteConn().String()
		ipPort1 := strings.Split(iraddr, ":")
		iladdr := ctx.InnerLocalConn().String()
		ipPort2 := strings.Split(iladdr, ":")
		if len(ipPort1) != 2 {
			t.Error("cannot get inner local address")
		}
		trace += ipPort2[0]

		data := ctx.DataBuffer()
		trace += string(data)
	})

	go func() {
		if err := proxyServer.Run(":9967"); err != nil {
			panic(err)
		}
	}()
	time.Sleep(time.Millisecond * 10)

	innerClient := NewProxyLiteClient(":9967")
	innerClient.SetLogger(logger)
	cancelFunc, done, err := innerClient.RegisterInnerService(
		RegisterInfo{
			OuterPort: 9968,
			InnerAddr: ":9966",
			Name:      "Echo",
			Message:   "TCP Echo Server",
		},
		ControlInfo{},
	)
	if err != nil {
		panic(err)
	}
	defer func() {
		cancelFunc()
		<-done
		time.Sleep(time.Millisecond * 10) // wait close
		proxyServer.Stop()
		if trace != "EchoTCP Echo Server127.0.0.1hello123127.0.0.1127.0.0.1hello123127.0.0.1hello123127.0.0.1127.0.0.1hello123127.0.0.1hello123127.0.0.1127.0.0.1hello123127.0.0.1hello123127.0.0.1127.0.0.1hello123127.0.0.1hello123127.0.0.1127.0.0.1hello123127.0.0.1hello123127.0.0.1127.0.0.1hello123127.0.0.1hello123127.0.0.1127.0.0.1hello123127.0.0.1hello123127.0.0.1127.0.0.1hello123127.0.0.1hello123127.0.0.1127.0.0.1hello123127.0.0.1hello123127.0.0.1127.0.0.1hello123127.0.0.1" {
			fmt.Println(trace)
			t.Error("hook does not work well")
		}
	}()
	time.Sleep(time.Millisecond * 10)

	user, err := net.Dial("tcp", ":9968")
	if err != nil {
		t.Fatal(err)
	}
	msg := "hello123"
	var data []byte
	for i := 0; i < 10; i++ {
		err := written(user, []byte(msg), 8)
		if err != nil {
			t.Error("write 1, ", err)
		}
		data, err = readn(user, 8)
		if err != nil {
			t.Error("read 1, ", err)
		}
		if string(data) != msg {
			t.Error("read 3, ", string(data))
		}
	}
	user.Close()
	time.Sleep(time.Millisecond * 100)
	select {
	case <-done:
		t.Error("unexpected quit")
	default:
	}
}

func TestHookContextAbortUser(t *testing.T) {

	logger := logrus.New()
	logger.Level = logrus.FatalLevel

	proxyServer := NewProxyLiteServer()
	proxyServer.SetLogger(logger)
	proxyServer.AddPort(9968, 9968)

	cnt := 0

	proxyServer.OnForwardTunnelToUser(func(ctx *Context) {
		cnt++
		if cnt == 2 {
			ctx.AbortUser()
		}
	})

	go func() {
		if err := proxyServer.Run(":9967"); err != nil {
			panic(err)
		}
	}()
	time.Sleep(time.Millisecond * 10)

	innerClient := NewProxyLiteClient(":9967")
	innerClient.SetLogger(logger)
	cancelFunc, done, err := innerClient.RegisterInnerService(
		RegisterInfo{
			OuterPort: 9968,
			InnerAddr: ":9966",
			Name:      "Echo",
			Message:   "TCP Echo Server",
		},
		ControlInfo{},
	)
	if err != nil {
		panic(err)
	}
	defer func() {
		cancelFunc()
		<-done
		time.Sleep(time.Millisecond * 10) // wait close
		proxyServer.Stop()
		if cnt != 2 {
			t.Error("hook does not work well")
		}
	}()
	time.Sleep(time.Millisecond * 10)

	user, err := net.Dial("tcp", ":9968")
	if err != nil {
		t.Fatal(err)
	}
	msg := "hello123"
	var data []byte
	for i := 0; i < 10; i++ {
		err := written(user, []byte(msg), 8)
		if err != nil {
			break
		}
		data, err = readn(user, 8)
		if err != nil {
			break
		}
		if string(data) != msg {
			break
		}
	}
	user.Close()
	time.Sleep(time.Millisecond * 100)
	select {
	case <-done:
		t.Error("unexpected quit")
	default:
	}
}

func TestHookContextAbortTunnel(t *testing.T) {

	logger := logrus.New()
	logger.Level = logrus.FatalLevel

	proxyServer := NewProxyLiteServer()
	proxyServer.SetLogger(logger)
	proxyServer.AddPort(9968, 9968)

	cnt := 0

	proxyServer.OnForwardTunnelToUser(func(ctx *Context) {
		cnt++
		if cnt == 2 {
			ctx.AbortTunnel()
		}
	})

	go func() {
		if err := proxyServer.Run(":9967"); err != nil {
			panic(err)
		}
	}()
	time.Sleep(time.Millisecond * 10)

	innerClient := NewProxyLiteClient(":9967")
	innerClient.SetLogger(logger)
	cancelFunc, done, err := innerClient.RegisterInnerService(
		RegisterInfo{
			OuterPort: 9968,
			InnerAddr: ":9966",
			Name:      "Echo",
			Message:   "TCP Echo Server",
		},
		ControlInfo{},
	)
	if err != nil {
		panic(err)
	}
	defer func() {
		cancelFunc()
		<-done
		time.Sleep(time.Millisecond * 10) // wait close
		proxyServer.Stop()
		if cnt != 2 {
			t.Error("hook does not work well")
		}
	}()
	time.Sleep(time.Millisecond * 10)

	user, err := net.Dial("tcp", ":9968")
	if err != nil {
		t.Fatal(err)
	}
	msg := "hello123"
	var data []byte
	for i := 0; i < 10; i++ {
		err := written(user, []byte(msg), 8)
		if err != nil {
			break
		}
		data, err = readn(user, 8)
		if err != nil {
			break
		}
		if string(data) != msg {
			break
		}
	}
	user.Close()
	time.Sleep(time.Millisecond * 100)
	select {
	case <-done:
	default:
		t.Error("unexpected continue")
	}
}
