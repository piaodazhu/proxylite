package main

import (
	"flag"

	"github.com/piaodazhu/proxylite"
)

func main() {
	var name string
	flag.StringVar(&name, "n", "abc", "service name")
	flag.Parse()
	cli := proxylite.NewProxyLiteClient(":9933")
	port, ok := cli.AnyPort()
	if !ok {
		panic("anyport() error")
	}
	_, done, err := cli.RegisterInnerService(proxylite.RegisterInfo{
		OuterPort: port,
		InnerAddr: ":123",
		Name: name,
		Message: "hello world",
	}, proxylite.ControlInfo{})
	if err != nil {
		panic(err)
	}
	<-done
}