package proxylite

import (
	"fmt"
	"testing"
)

type student struct {
	say  HookFunc
	name string
}

func newSayHello(msg string) HookFunc {
	return func(ctx *Context) {
		words = fmt.Sprintf("hello %s", msg)
	}
}

var words string

func TestHookBasic(t *testing.T) {
	alice := student{
		name: "alice",
	}
	alice.say = newSayHello(alice.name)
	words = ""
	alice.say.Trigger(&Context{})
	if words != fmt.Sprintf("hello %s", alice.name) {
		t.Error("trigger hook func failed")
	}
}
