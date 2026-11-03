// Package tracing adds OpenTelemetry server spans to gin requests |
// tracing 为 gin 请求添加 OpenTelemetry 服务端 span.
//
// # Notes | 说明
//
// Spans are named after the Gin route template (FullPath); unmatched paths
// fall back to "HTTP <method> route not found". Tracer provider and
// propagators default to the otel globals; override them with
// WithTracerProvider / WithPropagators.
// span 以 Gin 路由模板（FullPath）命名；未匹配路径回退为
// "HTTP <method> route not found"。TracerProvider 与传播器默认取 otel 全局值，
// 可用 WithTracerProvider / WithPropagators 覆盖。
//
// # Example | 示例
//
//	r.Use(tracing.Tracing("order-service"))
package tracing

import (
	"fmt"

	"github.com/gin-gonic/gin"
	otelcontrib "go.opentelemetry.io/contrib"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/propagation"
	semconv "go.opentelemetry.io/otel/semconv/v1.12.0"
	oteltrace "go.opentelemetry.io/otel/trace"
)

const (
	tracerKey  = "otel-tracer"
	tracerName = "otelgin"
)

type traceConfig struct {
	TracerProvider oteltrace.TracerProvider
	Propagators    propagation.TextMapPropagator
}

// TraceOption specifies instrumentation configuration options.
// TraceOption 指定埋点配置选项。
type TraceOption interface {
	apply(*traceConfig)
}

// TraceOptionFunc adapts a plain function to TraceOption.
// TraceOptionFunc 把普通函数适配为 TraceOption。
type TraceOptionFunc func(*traceConfig)

func (f TraceOptionFunc) apply(o *traceConfig) {
	f(o)
}

// WithPropagators specifies propagators to use for extracting
// information from the HTTP requests. If none are specified, global
// ones will be used.
// WithPropagators 指定从 HTTP 请求提取信息的传播器；不指定则用全局传播器。
func WithPropagators(propagators propagation.TextMapPropagator) TraceOption {
	return TraceOptionFunc(func(cfg *traceConfig) {
		cfg.Propagators = propagators
	})
}

// WithTracerProvider specifies a tracer provider to use for creating a tracer.
// If none is specified, the global provider is used.
// WithTracerProvider 指定创建 tracer 所用的 TracerProvider；不指定则用全局提供者。
func WithTracerProvider(provider oteltrace.TracerProvider) TraceOption {
	return TraceOptionFunc(func(cfg *traceConfig) {
		cfg.TracerProvider = provider
	})
}

// Tracing returns interceptor that will trace incoming requests.
// The service parameter should describe the name of the (virtual)
// server handling the request.
// Tracing 返回追踪入站请求的拦截器。service 参数应描述处理请求的（虚拟）服务名。
func Tracing(serviceName string, opts ...TraceOption) gin.HandlerFunc {
	cfg := traceConfig{}
	for _, opt := range opts {
		opt.apply(&cfg)
	}
	if cfg.TracerProvider == nil {
		cfg.TracerProvider = otel.GetTracerProvider()
	}
	tracer := cfg.TracerProvider.Tracer(
		tracerName,
		oteltrace.WithInstrumentationVersion(otelcontrib.SemVersion()),
	)
	if cfg.Propagators == nil {
		cfg.Propagators = otel.GetTextMapPropagator()
	}

	return func(c *gin.Context) {
		c.Set(tracerKey, tracer)
		savedCtx := c.Request.Context()
		defer func() {
			c.Request = c.Request.WithContext(savedCtx)
		}()
		ctx := cfg.Propagators.Extract(savedCtx, propagation.HeaderCarrier(c.Request.Header))
		route := c.FullPath()
		opts := []oteltrace.SpanStartOption{
			oteltrace.WithAttributes(semconv.NetAttributesFromHTTPRequest("tcp", c.Request)...),
			oteltrace.WithAttributes(semconv.EndUserAttributesFromHTTPRequest(c.Request)...),
			oteltrace.WithAttributes(semconv.HTTPServerAttributesFromHTTPRequest(serviceName, route, c.Request)...),
			oteltrace.WithSpanKind(oteltrace.SpanKindServer),
		}
		spanName := route
		if spanName == "" {
			spanName = fmt.Sprintf("HTTP %s route not found", c.Request.Method)
		}
		ctx, span := tracer.Start(ctx, spanName, opts...)
		defer span.End()

		// pass the span through the request context
		// 把 span 放入请求 context 传递
		c.Request = c.Request.WithContext(ctx)

		// serve the request to the next interceptor
		// 把请求交给下一个拦截器
		c.Next()

		status := c.Writer.Status()
		attrs := semconv.HTTPAttributesFromHTTPStatusCode(status)
		spanStatus, spanMessage := semconv.SpanStatusFromHTTPStatusCode(status)
		span.SetAttributes(attrs...)
		span.SetStatus(spanStatus, spanMessage)
		if len(c.Errors) > 0 {
			span.SetAttributes(attribute.String("gin.errors", c.Errors.String()))
		}
	}
}
