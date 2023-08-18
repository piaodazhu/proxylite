package main

import (
	"encoding/json"
	"fmt"

	"github.com/piaodazhu/proxylite"
	"github.com/tidwall/pretty"
)

func main() {
	infos, err := proxylite.DiscoverServices(":9933")
	if err != nil {
		panic(err)
	}
	raw, _ := json.Marshal(infos)
	fmt.Println(string(pretty.Pretty(raw)))
}
