package main

import (
	"log"
	"time"

	"github.com/piaodazhu/proxylite"
)

func main() {
	client := proxylite.NewProxyLiteClient(":9933")
	log.Print(client.AvaliablePorts())
	log.Print(client.ActiveServices())
	log.Print(client.AnyPort())
	log.Print(client.AnyPort())
	log.Print(client.AnyPort())

	log.Print(client.RegisterInnerService(
		proxylite.RegisterInfo{
			OuterPort: 9931,
			InnerAddr: "127.0.0.1:6379",
			Name:      "redis",
			Message:   "redis kv",
		},
		proxylite.ControlInfo{
			MaxServeConn: 2,
			MaxServeCount: 4,
			MaxServeTime: 600,
		},
	))

	log.Print(client.AvaliablePorts())
	log.Print(client.ActiveServices())
	log.Print(client.GetRegisterEntryByPort(9931))
	log.Print(client.GetRegisterEntryByName("redis"))
	entry, ok := client.GetRegisterEntryByName("redis")

	if !ok {
		log.Fatal("register failed")
		return
	}
	<-entry.Done
	time.Sleep(time.Microsecond * 10)
	log.Print(client.AvaliablePorts())
	log.Print(client.ActiveServices())
	log.Print(client.GetRegisterEntryByPort(9931))
	log.Print(client.GetRegisterEntryByName("redis"))

	log.Print("ALL DONE!")
}
