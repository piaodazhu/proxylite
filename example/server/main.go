package main
import "github.com/piaodazhu/proxylite"

func main() {
	server := proxylite.NewProxyLiteServer()
	server.AddPort(9930, 9932)
	panic(server.Run(":9933"))
}
