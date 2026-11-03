// Package chain assembles middlewares into a single decorator | chain 将多个中间件组装成单个装饰器.
//
// # Notes | 说明
//
// The only export is [Chain], generic over the handler type H and the
// middleware type M (~func(H) H). The first argument is the outermost
// middleware (executed first) and the last is the innermost:
//
//	Chain(m1, m2, m3)(h) == m1(m2(m3(h)))
//
// 唯一的导出是 [Chain]，对处理器类型 H 与中间件类型 M（~func(H) H）泛型。
// 第一个参数是最外层中间件（最先执行），最后一个是最内层。
package chain

// Chain assembles middlewares so that the first argument is the outermost
// (executed first) and the last is the innermost (closest to the handler):
//
//	Chain(m1, m2, m3)(h) == m1(m2(m3(h)))
//
// H is the handler type and M is the middleware decorator type ~func(H) H. This
// collapses the previously duplicated middleware.Chain and resilience.Chain
// implementations into one generic helper.
//
// Chain 将多个中间件组装起来：第一个参数是最外层（最先执行），最后一个是最内层
// （最贴近处理器）。H 为处理器类型，M 为中间件装饰器类型 ~func(H) H；
// 它把此前重复实现的 middleware.Chain 与 resilience.Chain 收敛为一个泛型工具。
func Chain[H any, M ~func(H) H](outer M, others ...M) M {
	return func(next H) H {
		for i := len(others) - 1; i >= 0; i-- {
			next = others[i](next)
		}
		return outer(next)
	}
}
