package main
import "github.com/piaodazhu/proxylite"

func main() {
	server := proxylite.NewProxyLiteServer()
	server.AddPort(9940, 9948)
	panic(server.Run(":9933"))
}
