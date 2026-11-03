// Package signal registers SIGTERM and SIGINT shutdown handling | 注册
// SIGTERM 与 SIGINT 的关停处理.
//
// # Notes | 说明
//
// SetupSignalHandler may be called only once; a second call panics. It
// returns a channel closed on the first signal, and a second signal exits
// the program with code 1. WaitExit blocks until any shutdown signal
// arrives. On POSIX systems SIGHUP is observed too; on Windows only SIGINT
// and SIGTERM.
// SetupSignalHandler 只能调用一次，第二次会 panic；首个信号到达时关闭返回
// 的通道，第二个信号直接以退出码 1 结束进程。WaitExit 阻塞等待任一关停
// 信号。POSIX 上还会监听 SIGHUP；Windows 只监听 SIGINT 与 SIGTERM。
//
// # Features | 功能
//
// - SetupSignalHandler returns a one-shot stop channel | 一次性 stop 通道
// - WaitExit blocks until a shutdown signal | 阻塞至任一关停信号
//
// # Example | 示例
//
//	stopCh := signal.SetupSignalHandler()
//	<-stopCh
//	// drain workers, flush, then return from main
package signal

import (
	"os"
	"os/signal"
)

var onlyOneSignalHandler = make(chan struct{})

// SetupSignalHandler registers for SIGTERM and SIGINT. A stop channel is
// returned which is closed on one of these signals. If a second signal is
// caught, the program is terminated with exit code 1.
// SetupSignalHandler 注册 SIGTERM 与 SIGINT 的处理：返回的 stop 通道会在收到
// 其中一个信号时关闭；若收到第二个信号，程序以退出码 1 直接终止。
func SetupSignalHandler() (stopCh <-chan struct{}) {
	// panics when called twice
	// 第二次调用会 panic
	close(onlyOneSignalHandler)

	stop := make(chan struct{})
	c := make(chan os.Signal, 2)
	signal.Notify(c, shutdownSignals...)
	go func() {
		<-c
		close(stop)
		<-c
		// second signal. Exit directly.
		// 第二个信号，直接退出。
		os.Exit(1)
	}()

	return stop
}

// WaitExit blocks until any shutdown signal arrives | WaitExit 阻塞直到任一关停信号到来.
func WaitExit() {
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, shutdownSignals...)
	<-signals
}
