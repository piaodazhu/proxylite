package main

import (
	"flag"
	"fmt"

	"github.com/piaodazhu/proxylite"
	"github.com/spf13/viper"
)

func main() {
	var conf string
	flag.StringVar(&conf, "c", "frps.ini", "config file")
	flag.Parse()
	viper.SetConfigFile(conf)
	if err := viper.ReadInConfig(); err != nil {
		fmt.Println(err)
	}
	port := viper.GetInt("common.bind_port")

	server := proxylite.NewProxyLiteServer()
	server.AddPort(40000, 50000)
	if err := server.Run(fmt.Sprintf(":%d", port)); err != nil {
		fmt.Println(err)
	}
}
