package main

import (
	"flag"
	"fmt"
	"sync"

	"github.com/piaodazhu/proxylite"
	"github.com/sirupsen/logrus"
	"gopkg.in/ini.v1"
)

type CommonConfig struct {
	ServerAddress string `ini:"server_addr"`
	ServerPort    int    `ini:"server_port"`
}

type ClientConfig struct {
	Name       string
	Type       string `ini:"type"`
	LocalIp    string `ini:"local_ip"`
	LocalPort  int    `ini:"local_port"`
	RemotePort int    `ini:"remote_port"`
}

var common CommonConfig
var items []ClientConfig

func parseConfig(conf string) {
	f, err := ini.Load(conf)
	if err != nil {
		panic(err)
	}

	secs := f.Sections()
	for i := range secs {
		switch secs[i].Name() {
		case "DEFAULT":
		case "common":
			err := secs[i].MapTo(&common)
			if err != nil {
				panic(err)
			}
		default:
			cli := ClientConfig{}
			err := secs[i].MapTo(&cli)
			if err != nil {
				panic(err)
			}
			if cli.Type != "tcp" {
				panic("frplite only support tcp: " + secs[i].Name())
			}
			cli.Name = secs[i].Name()
			items = append(items, cli)
		}
	}
}

func main() {
	var conf string
	flag.StringVar(&conf, "c", "frpc.ini", "config file")
	flag.Parse()

	parseConfig(conf)

	client := proxylite.NewProxyLiteClient(fmt.Sprintf("%s:%d", common.ServerAddress, common.ServerPort))
	ports, succ := client.AvailablePorts()
	if !succ {
		panic("cannot ask frplite server for available ports")
	}
	portSet := map[int]struct{}{}
	for _, p := range ports {
		portSet[int(p)] = struct{}{}
	}

	// check ports
	for _, reg := range items {
		if _, ok := portSet[reg.RemotePort]; !ok {
			panic("port not available from frplite server: " + fmt.Sprint(reg.RemotePort))
		}
	}

	wg := sync.WaitGroup{}
	for _, item := range items {
		wg.Add(1)
		go func(reg ClientConfig) {
			for {
				_, done, err := client.RegisterInnerService(proxylite.RegisterInfo{
					OuterPort: uint32(reg.RemotePort),
					InnerAddr: fmt.Sprintf("%s:%d", reg.LocalIp, reg.LocalPort),
					Name:      reg.Name,
					Message:   "frplite client",
				}, proxylite.ControlInfo{})
				if err != nil {
					logrus.Warnf("%s register failed: %v", reg.Name, err)
					break
				}
				<-done
			}
			wg.Done()
		}(item)
	}
	wg.Wait()
}
