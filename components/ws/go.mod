module github.com/apus-run/gala/components/ws

go 1.25

replace github.com/apus-run/gala/components/ws => ../ws

require (
	github.com/gorilla/websocket v1.5.3
	go.uber.org/mock v0.5.2
)
