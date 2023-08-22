package proxylite

type HookFunc func(ctx *Context)

func (hf HookFunc) Trigger(ctx *Context) {
	if hf != nil && ctx != nil {
		hf(ctx)
	}
}
