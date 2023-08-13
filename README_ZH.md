[English](./README.md)|[中文](./README_ZH.md)

---- 以下中文文档由ChatGPT生成 ----

## proxylite
`proxylite` 是一个用于实现 NAT 或防火墙穿越的动态 TCP 反向代理 Golang 包。使用 `proxylite`，您可以轻松将网络穿越功能集成到您的项目中。

与众所周知的 [frp](https://github.com/fatedier/frp) 不同，`proxylite` 不是一组可运行的程序，而是一个轻量级的包，提供了良好的灵活性，并且很容易集成到 Golang 项目中。当然，构建一组可运行的程序也同样简单。

## 为什么选择 proxylite

有一天，我需要为[我的项目](https://github.com/piaodazhu/Octopoda)添加 TCP NAT 穿越功能，所以我尝试引入 [frp](https://github.com/fatedier/frp) 来实现这个功能。但我意识到，frp 需要读取配置文件并启动进程，这不适用于我的需求：
- 首先，启动一个进程会向项目引入一个独立的服务，使得**维护**变得更加困难。
- 此外，这种方法不允许**轻松收集日志信息**。
- 最重要的是，我需要**动态地建立按需连接**，而编辑配置文件，然后重新启动进程显然不够优雅。

那为什么不编写一个包来使这变得更加优雅呢？proxylite 应运而生。它的主要特点如下：
1. 易于集成到代码中。提供了服务器和客户端结构。只需导入此包，然后在需要时注册隧道。
2. 动态按需反向代理。**一次注册，一个端口，一个用户，一个 TCP 连接。**
3. 服务注册和发现。
4. 支持自定义钩子。

## 概念
```
  +-----------------------+   +-----------------------+
  | 内部服务 <--- 内部客户端 +---+> 监听端口 <--- 外部端口 <+---- 用户 
  +-----------------------+   +-----------------------+
           NAT 节点         ...       公共服务器             任何用户
```

## 快速开始

首先，您应该导入 proxylite：
```golang
import "github.com/piaodazhu/proxylite"
```

让我们创建一个服务器：
```golang
package main
import "github.com/piaodazhu/proxylite"

func main() {
	server := proxylite.NewProxyLiteServer()
	server.AddPort(9930, 9932)
	panic(server.Run(":9933"))
}
```

这些代码创建了一个 proxylite 服务器，并添加了可用的外部端口 9930-9932（注意不是 9930 和 9932，而是从 9930 到 9932），然后运行服务器。服务器会阻塞在端口 9939 上进行监听，内部客户端将拨打该端口，而服务器发现也将绑定到该端口。

然后，我们创建一个内部客户端：
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
			OuterPort: 9931,
			InnerAddr: ":22",
			Name:      "ssh",
			Message:   "ssh 登录",
		},
	)
    if err != nil {
        log.Fatal(err)
        return
    }

	entry, ok := client.GetRegisterEntryByName("ssh")
	if !ok {
        log.Fatal("注册失败")
		return
	}
	<-entry.Done
    log.Print("再见 :)")
}
```

这些代码创建了一个内部客户端，绑定到服务器 `"0.0.0.0:9933"`。然后，我们将一个内部服务注册到服务器上：
```golang
proxylite.RegisterInfo{
    OuterPort: 9931,    // 表示我们要将服务器的 9931 端口映射到我们的内部服务
    InnerAddr: ":22",   // 表示内部服务为 127.0.0.1:22，例如默认的 SSH 端口
    Name:      "ssh",   // 服务名称
    Message:   "ssh 登录", // 自定义信息
},
```
然后，如果注册成功，我们获取注册条目。最后，我们通过读取通道等待注册完成。

## 教程

### 服务器
```golang
func NewProxyLiteServer(portIntervals ...[2]int) *ProxyLiteServer
```
使用可用的端口间隔创建代理服务器。

```golang
func (s *ProxyLiteServer) AddPort(from, to int) bool
```
使用可用的端口间隔创建代理服务器。如果端口无效，返回 false。

```golang
func (s *ProxyLiteServer) SetLogger(logger *log.Logger)
```
为服务器设置自定义的 logrus 日志记录器。

```golang
func (s *ProxyLiteServer) Run(addr string) error
```
运行服务器，并使其监听给定的地址。

### 客户端

```golang
func NewProxyLiteClient(serverAddr string) *ProxyLiteClient
```
创建一个与代理服务器绑定的内部客户端。

```golang
func (c *ProxyLiteClient) AvaliablePorts() ([]int, bool)
```
从代理服务器获取可用端口。

```golang
func (c *ProxyLiteClient) AnyPort() (int, bool)
```
从代理服务器获取一个随机可用端口。

```golang
type ServiceInfo struct {
	Port    int
	Name    string
	Message string
	Busy    bool
	Birth   time.Time
}

func (c *ProxyLiteClient) ActiveServices() ([]ServiceInfo, error)
```
从代理服务器发现所有活动服务。

```golang
type RegisterInfo struct {
	OuterPort int
	InnerAddr string
	Name      string
	Message   string
}

func (c *ProxyLiteClient) RegisterInnerService(info RegisterInfo) error
```
将内

部服务器注册到代理服务器的外部端口。

```golang
type RegisterEntry struct {
	// 基本信息
	Info   RegisterInfo
	// 取消函数
	Cancel func()
	// 完成通道
	Done   <-chan struct{}
}

func (c *ProxyLiteClient) GetRegisterEntryByName(name string) (*RegisterEntry, bool) 
func (c *ProxyLiteClient) GetRegisterEntryByPort(port int) (*RegisterEntry, bool)
```
通过名称或端口获取 RegisterEntry。RegisterEntry 可用于取消隧道或等待完成。

```golang
func (c *ProxyLiteClient) SetLogger(logger *log.Logger)
```
为内部客户端设置自定义的 logrus 日志记录器。

### 其他

```golang
func AskFreePort(addr string) ([]int, error)
```
从给定地址的代理服务器请求可用的空闲端口。

```golang
func DiscoverServices(addr string) ([]ServiceInfo, error)
```
从给定地址的代理服务器发现所有活动服务。

## 贡献

欢迎提出问题或拉取请求，以使这个项目变得更好。 🌈