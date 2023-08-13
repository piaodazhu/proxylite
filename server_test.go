package main

import (
	"fmt"
	"testing"
)

func TestServer(t *testing.T) {
	server := NewProxyLiteServer()
	server.AddPort(9930, 9932)
	server.Run(":9933")
}

func TestRegister(t *testing.T) {
	server := NewProxyLiteServer()
	server.AddPort(9930, 9932)
	server.Run(":9933")
}

type Student struct {
	Name string
	Age  int
}

func printInterface(obj interface{}) {
	fmt.Printf("%#v", obj)
}

func TestPrintInterface(t *testing.T) {
	stu := &Student{
		Name: "Alice",
		Age:  11,
	}
	printInterface(stu)
}
