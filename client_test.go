package proxylite

import (
	"testing"
	"time"
)

func TestClient(t *testing.T) {
	client := NewProxyLiteClient(":9933")
	t.Log(client.AvaliablePorts())
	t.Log(client.ActiveServices())
	t.Log(client.AnyPort())
	t.Log(client.AnyPort())
	t.Log(client.AnyPort())

	t.Log(client.RegisterInnerService(
		RegisterInfo{
			OuterPort: 9931,
			InnerAddr: ":22",
			Name:      "ssh",
			Message:   "ssh login",
		},
	))

	t.Log(client.AvaliablePorts())
	t.Log(client.ActiveServices())
	t.Log(client.GetRegisterEntryByPort(9931))
	t.Log(client.GetRegisterEntryByName("ssh"))
	entry, ok := client.GetRegisterEntryByName("ssh")

	if !ok {
		t.Error("register failed")
		return
	}
	<-entry.Done
	time.Sleep(time.Microsecond * 10)
	t.Log(client.AvaliablePorts())
	t.Log(client.ActiveServices())
	t.Log(client.GetRegisterEntryByPort(9931))
	t.Log(client.GetRegisterEntryByName("ssh"))
}
