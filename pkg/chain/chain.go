package chain

// Chain assembles middlewares so that the first argument is the outermost
// (executed first) and the last is the innermost (closest to the handler):
//
//	Chain(m1, m2, m3)(h) == m1(m2(m3(h)))
//
// H is the handler type and M is the middleware decorator type ~func(H) H. This
// collapses the previously duplicated middleware.Chain and resilience.Chain
// implementations into one generic helper.
func Chain[H any, M ~func(H) H](outer M, others ...M) M {
	return func(next H) H {
		for i := len(others) - 1; i >= 0; i-- {
			next = others[i](next)
		}
		return outer(next)
	}
}
