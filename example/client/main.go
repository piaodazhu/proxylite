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
			InnerAddr: ":22",
			Name:      "ssh",
			Message:   "ssh login",
		},
	))

	log.Print(client.AvaliablePorts())
	log.Print(client.ActiveServices())
	log.Print(client.GetRegisterEntryByPort(9931))
	log.Print(client.GetRegisterEntryByName("ssh"))
	entry, ok := client.GetRegisterEntryByName("ssh")

	if !ok {
		log.Fatal("register failed")
		return
	}
	<-entry.Done
	time.Sleep(time.Microsecond * 10)
	log.Print(client.AvaliablePorts())
	log.Print(client.ActiveServices())
	log.Print(client.GetRegisterEntryByPort(9931))
	log.Print(client.GetRegisterEntryByName("ssh"))

	log.Print("ALL DONE!")
}
