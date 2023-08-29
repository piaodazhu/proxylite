package proxylite

import (
	"net"
	"strings"
	"testing"
	"time"
)

var msg = strings.Repeat("hello", 100)

func init() {
	if err := newTcpEchoServer(":9969", len(msg)); err != nil {
		panic(err)
	}
}

func BenchmarkProxylite(b *testing.B) {
	proxyServer := NewProxyLiteServer()
	proxyServer.SetLogger(nil)
	proxyServer.AddPort(9968, 9968)
	go func() {
		if err := proxyServer.Run(":9967"); err != nil {
			panic(err)
		}
	}()
	time.Sleep(time.Millisecond * 10)

	innerClient := NewProxyLiteClient(":9967")
	innerClient.SetLogger(nil)

	cancelFunc, done, err := innerClient.RegisterInnerService(
		RegisterInfo{
			OuterPort: 9968,
			InnerAddr: ":9969",
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
		b.Fatal(err)
	}

	b.StartTimer()
	for i := 0; i < b.N; i++ {
		err := written(user, []byte(msg), len(msg))
		if err != nil {
			b.Error("write 1, ", err)
		}
		_, err = readn(user, len(msg))
		if err != nil {
			b.Error("read 1, ", err)
		}
	}
	b.StopTimer()
	user.Close()
	time.Sleep(time.Millisecond * 100)
	select {
	case <-done:
		b.Error("unexpected quit")
	default:
	}
}

func BenchmarkNoProxy(b *testing.B) {
	user, err := net.Dial("tcp", ":9969")
	if err != nil {
		b.Fail()
	}
	b.StartTimer()
	for i := 0; i < b.N; i++ {
		err := written(user, []byte(msg), len(msg))
		if err != nil {
			b.Error("write 1, ", err)
		}
		_, err = readn(user, len(msg))
		if err != nil {
			b.Error("read 1, ", err)
		}
	}
	b.StopTimer()
	user.Close()
}
