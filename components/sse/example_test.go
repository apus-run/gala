package sse_test

import (
	"fmt"
	"net/http/httptest"
	"strings"

	"github.com/apus-run/gala/components/sse"
)

func ExampleWriter() {
	rec := httptest.NewRecorder() // in a real handler this is the http.ResponseWriter
	w, _ := sse.NewWriter(rec)

	_ = w.Send(sse.Event{ID: "1", Type: "greeting", Data: []byte("hello\nworld")})
	_ = w.Comment("keep-alive")

	fmt.Print(rec.Body.String())
	// Output:
	// id: 1
	// event: greeting
	// data: hello
	// data: world
	//
	// : keep-alive
}

func ExampleReader() {
	body := "id: 1\nevent: greeting\ndata: hello\ndata: world\n\n: ping\n\ndata: bye\n\n"
	r := sse.NewReader(strings.NewReader(body))

	for {
		e, err := r.Next()
		if err != nil {
			fmt.Println(err)
			break
		}
		fmt.Printf("%s %q %q\n", e.Type, e.ID, e.Data)
	}
	fmt.Println("resume from:", r.LastEventID())
	// Output:
	// greeting "1" "hello\nworld"
	// message "" "bye"
	// EOF
	// resume from: 1
}
