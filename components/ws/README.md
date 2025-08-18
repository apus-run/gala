## WebSocket

`ws` is a websocket library based on [gorilla/websocket](https://github.com/gorilla/websocket), support automatic client reconnection.

<br>

### Example of use

#### 1. Default setting

**Server side code example:**

```go
package main

import (
    "context"
    "log"
    "github.com/apus-run/gala/components/ws"
    "github.com/gin-gonic/gin"
)

func main() {
    r := gin.Default()
	
    r.GET("/ws", func(c *gin.Context) {
        s := ws.NewServer(c.Writer, c.Request, loopReceiveMessage) // default setting
        err := s.Run(context.Background())
        if err != nil {
            log.Println("webSocket server error:", err)
        }
    })
	
    err := r.Run(":8080")
    if err != nil {
        panic(err)
    }
}

func loopReceiveMessage(ctx context.Context, conn *ws.Conn) {
    for {
        messageType, message, err := conn.ReadMessage()
        if err != nil { // release connection
            return
        }

        // handle message
        log.Println(messageType, message)
    }
}
```

<br>

**Client side code example:**

```go
package main

import (
    "strconv"
    "log"
    "time"
    "github.com/apus-run/gala/components/ws"
    "github.com/gorilla/websocket"
)

var wsURL = "ws://localhost:8080/ws"

func main() {
    c, err := ws.NewClient(wsURL) // default setting
    if err != nil {
        log.Println("connect error:", err)
        return
    }
    defer c.CloseConnection()

    go func() {
        for {
            _, message, err := c.GetConnection().ReadMessage()
            if err != nil {
                log.Println("client read error:", err)
                return
            }
            log.Printf("client received: %s", message)
        }
    }()
    
    for i := 0; i < 5; i++ {
        data := "Hello, World " + strconv.Itoa(i)
        err = c.GetConnection().WriteMessage(websocket.TextMessage, []byte(data))
        if err != nil {
            log.Println("write error:", err)
        }
        time.Sleep(100 * time.Millisecond)
    }
}
```

<br>

#### 2. Custom setting

**Server side custom setting**, options such as `ws.Upgrader`, `ws.WithNoClientPingTimeout`, `ws.WithServerLogger`, `ws.WithResponseHeader` can be set.

```go
package main

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/apus-run/gala/components/ws"
)

func main() {
	r := gin.Default()
	ug := &websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool {
			return true
		},
	}
	r.GET("/ws", func(c *gin.Context) {
		s := ws.NewServer(c.Writer, c.Request, loopReceiveMessage,
			ws.WithUpgrader(ug),
			ws.WithNoClientPingTimeout(time.Minute), // client side must send ping message in every 1 minutes
		)
		err := s.Run(context.Background())
		if err != nil {
			log.Println("webSocket server error:", err)
		}
	})

	err := r.Run(":8080")
	if err != nil {
		panic(err)
	}
}

func loopReceiveMessage(ctx context.Context, conn *ws.Conn) {
	for {
		messageType, message, err := conn.ReadMessage()
		if err != nil {
			
			logger.Warn("ReadMessage error", logger.Err(err))
			return
		}
		logger.Infof("server side received: %s", message)

		switch messageType {
		case websocket.TextMessage:
			err = conn.WriteMessage(messageType, message)
			if err != nil {
				logger.Warn("WriteMessage error", logger.Err(err))
				continue
			}

		case websocket.PingMessage:
			err = conn.WriteMessage(websocket.PongMessage, []byte("pong"))
			if err != nil {
				logger.Warn("Write pong message error:", logger.Err(err))
				continue
			}
		default:
			logger.Warnf("Unknown message type: %d", messageType)
		}
	}
}
```

<br>

**Client side custom setting**, options such as `ws.Dialer`, `ws.WithPing`, `ws.WithClientLogger`, `ws.WithRequestHeader` can be set.

```go
package main

import (
	"strconv"
	"time"

	"github.com/gorilla/websocket"
	"github.com/apus-run/gala/components/ws"
)

var wsURL = "ws://localhost:8080/ws"

func main() {
	c, err := ws.NewClient(wsURL,
		ws.WithPing(time.Second*20), //  It is recommended that the ping timeout time set by the server be less than 1/2
	)
	if err != nil {
		log.Println("connect error:", err)
		return
	}
	defer c.CloseConnection()

	go clientLoopReadMessage(c)

	i := 0
	for {
		time.Sleep(time.Second * 3)
		i++
		data := "Hello, World " + strconv.Itoa(i)
		err = c.GetConnection().WriteMessage(websocket.TextMessage, []byte(data))
		if err != nil {
			log.Println("write error:", err)
		}
	}
}

func clientLoopReadMessage(c *ws.Client) {
	for {
		select {
		case <-c.GetCtx().Done():
			return
		default:
			_, message, err := c.GetConnection().ReadMessage()
			if err != nil {
				log.Println("read error:", err)
				time.Sleep(time.Second * 5)
				continue
			}
			log.Printf("client received: %s", message)
		}

	}
}
```
