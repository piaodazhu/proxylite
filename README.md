[![Go Reference](https://pkg.go.dev/badge/github.com/piaodazhu/proxylite.svg)](https://pkg.go.dev/github.com/piaodazhu/proxylite)
[![Go Report Card](https://goreportcard.com/badge/github.com/piaodazhu/proxylite)](https://goreportcard.com/report/github.com/piaodazhu/proxylite)
[![codecov](https://codecov.io/gh/piaodazhu/proxylite/graph/badge.svg?token=WdqA7lKG0C)](https://codecov.io/gh/piaodazhu/proxylite)

[English](./README.md)|[中文](./README_ZH.md)
## proxylite
`proxylite` is a dynamic TCP reverse proxy Golang package for NAT or firewall traversal. It is god damn easy to integrate network traversal feature into your project with proxylite. 

Different from well-known [frp](https://github.com/fatedier/frp), `proxylite` is not a set of runnable programs, but a lightweight package, which provides good flexibility and is easy to integrate in golang projects. Of course, build a set of runnable programs is also a piece of cake.

## Why proxylite

One day, I needed to add TCP NAT traversal to [my project](https://github.com/piaodazhu/Octopoda), so I tried to introduce [frp](https://github.com/fatedier/frp) to implement this feature. But I realized that frp needs to read configuration files and start processes, which is not a good fit for my needs: 
- First of all, starting a process introduces a new independent service to the project, which makes it more difficult to **maintain**. 
- Also, this approach does not allow for **easy collection of logging information**. 
- Most importantly, I needed to **dynamically establish on-demand connections**, and editing the configuration file and then restarting the process was clearly inelegant.

So why not write a package to make this more elegant? proxylite was born. Its main features are listed below:
1. Easy to integrate into code. Both server and client structures are provided. Just import this package then register tunnels whenever you want.
2. Dynamic on-demand reverse proxy with serivce controlling (maxOnline, maxServe and deadline).
3. Service discovery and status display.
4. Customized hooks are support.

## Concepts 
```
  +---------------------------------+   +-----------------------------------+
  | Inner Service <--- Inner Client +---+> Listenning Port <--- Outer Port <+---- User 
  +---------------------------------+   +-----------------------------------+
               NAT Nodes             ...             Public Server               Any User
```

## Quick Start

First, you should import proxylite:
```golang
import "github.com/piaodazhu/proxylite"
```

Let's create a server:
```golang
package main
import "github.com/piaodazhu/proxylite"

func main() {
    server := proxylite.NewProxyLiteServer()
    server.AddPort(9930, 9932)

    server.Run(":9933")
}
```

These code create a proxylite server, and add available outer port 9930-9932 (Note that it is not 9930 and 9932, but from 9930 to 9932), then run the server. The server is blocked listening on port 9939, inner client will dial this port and server discovery also bind this port.

Then, we create a inner client:
```golang
package main

import (
    "log"

    "github.com/piaodazhu/proxylite"
)

func main() {
    client := proxylite.NewProxyLiteClient("0.0.0.0:9933")
    err := client.RegisterInnerService(
        proxylite.RegisterInfo{
            OuterPort: 9931,     // map ssh service to serverIp:9931
            InnerAddr: ":22",
            Name:      "ssh",
            Message:   "ssh login",
        },
        proxylite.ControlInfo{
			MaxServeConn:  3,     // 3 users at the same time
			MaxServeCount: 1000,  // close after 1000 servings
			MaxServeTime:  600,   // close after 10min
		},
    )
    if err != nil {
        log.Fatal(err)
        return
    }

    entry, ok := client.GetRegisterEntryByName("ssh")
    if !ok {
        log.Fatal("registration failed")
        return
    }
    <-entry.Done
    log.Print("BYE :)")
}
```

These code create a inner client, binding with server `"0.0.0.0:9933"`. Then we register a inner service to the server:
```golang
proxylite.RegisterInfo{
    OuterPort: 9931,    // means we want map server's 9931 port to our inner service 
    InnerAddr: ":22",   // means inner service is 127.0.0.1:22. e.g. default ssh port.
    Name:      "ssh",   // service name
    Message:   "ssh login", // customized information
},
```
Then we get the registration entry if the registration is success. Finally we wait it done by reading channel.

## frplite

`./frplite/frpc` and `./frplite/frps` are 2 executable programs like `frps` and `frpc`. They can be configured and run like `frp` but only support tcp.

## Tutorial

### Server
```golang
func NewProxyLiteServer(portIntervals ...[2]int) *ProxyLiteServer
```
Create a Proxy server with available ports intervals.

```golang
func (s *ProxyLiteServer) AddPort(from, to int) bool
```
Create a Proxy server with available ports intervals. Return false if port is invalid.

```golang
func (s *ProxyLiteServer) SetLogger(logger *log.Logger)
```
Set customized logrus logger the the server. 

```golang
func (s *ProxyLiteServer) Run(addr string) error
```
Run the server and let it listen on given address. 

### client

```golang
func NewProxyLiteClient(serverAddr string) *ProxyLiteClient
```
Create a inner client binding with a proxy server.

```golang
func (c *ProxyLiteClient) AvailablePorts() ([]int, bool)
```
Get available ports from proxy server.

```golang
func (c *ProxyLiteClient) AnyPort() (int, bool)
```
Get a random available port from proxy server.

```golang
type ServiceInfo struct {
	Port    uint32
	Name    string
	Message string
	Birth   time.Time

	Online       uint32    // online user count
	Capacity     uint32    // max concurrency user count
	AlreadyServe uint32    // already served user number
	TotalServe   uint32    // total user number can serve
	DeadLine     time.Time // time to close this port
}

func (c *ProxyLiteClient) ActiveServices() ([]ServiceInfo, error)
```
Discover all active services from proxy server.


```golang
type RegisterInfo struct {
    OuterPort int
    InnerAddr string
    Name      string
    Message   string
}

type ControlInfo struct {
	MaxServeTime  uint32
	MaxServeConn  uint32
	MaxServeCount uint32
}

func (c *ProxyLiteClient) RegisterInnerService(info RegisterInfo, 
    ctrl ControlInfo) (func(), chan struct{}, error)
```
Register inner server to proxy server's outer port.

```golang
type RegisterEntry struct {
    // Basic Info
    Info   RegisterInfo
    // Cancel function
    Cancel func()
    // Done channel
    Done   <-chan struct{}
}

func (c *ProxyLiteClient) GetRegisterEntryByName(name string) (*RegisterEntry, bool) 
func (c *ProxyLiteClient) GetRegisterEntryByPort(port int) (*RegisterEntry, bool)
```
Get RegisterEntry by name or port. RegisterEntry can be used to canncel tunnel or wait done.

```golang
func (c *ProxyLiteClient) SetLogger(logger *log.Logger)
```
Set customized logrus logger for the inner client. 

### Others

```golang
func AskFreePort(addr string) ([]int, error)
```
Ask available free port from proxy server with given address.


```golang
func DiscoverServices(addr string) ([]ServiceInfo, error)
```
Discover all active services from proxy server with given address.

## Contributing

Feel free to open issues or pull requests to make this project better. 🌈
