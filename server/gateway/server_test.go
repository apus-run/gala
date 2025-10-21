package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"testing"

	grpcServer "github.com/apus-run/gala/server/grpc"
	pb "github.com/apus-run/gala/server/internal/testdata/helloworld"
	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// service is used to implement helloworld.GreeterServer.
type service struct {
	pb.UnimplementedGreeterServer
}

func (s *service) SayHelloStream(streamServer pb.Greeter_SayHelloStreamServer) error {
	var cnt uint
	for {
		in, err := streamServer.Recv()
		if err != nil {
			return err
		}
		if in.Name == "error" {
			panic(fmt.Sprintf("invalid argument %s", in.Name))
		}
		if in.Name == "panic" {
			panic("server panic")
		}
		err = streamServer.Send(&pb.HelloReply{
			Message: fmt.Sprintf("hello %s", in.Name),
		})
		if err != nil {
			return err
		}
		cnt++
		if cnt > 1 {
			return nil
		}
	}
}

// SayHello implements helloworld.GreeterServer
func (s *service) SayHello(_ context.Context, in *pb.HelloRequest) (*pb.HelloReply, error) {
	if in.Name == "error" {
		panic(fmt.Sprintf("invalid argument %s", in.Name))
	}
	if in.Name == "panic" {
		panic("server panic")
	}
	return &pb.HelloReply{Message: fmt.Sprintf("Hello %+v", in.Name)}, nil
}

func runServer() {
	ctx := context.Background()
	go func() {
		srv := grpcServer.NewServer(grpcServer.WithAddress(":8080"))
		pb.RegisterGreeterServer(srv, &service{})
		if err := srv.Start(ctx); err != nil {
			panic(err)
		}
	}()

	go func() {
		// Create a client connection to the gRPC core we just started
		// This is where the gRPC-Gateway proxies the requests
		conn, err := grpc.NewClient(":8080", grpc.WithTransportCredentials(insecure.NewCredentials()))
		if err != nil {
			log.Fatalln("Failed to dial core:", err)
		}

		paralusJSON := NewParalusJSON()
		gw, err := NewServer(
			ctx,
			WithAddress(":9999"),
			WithConn(conn),
			WithServeMuxOpts(runtime.WithMarshalerOption(jsonContentType, paralusJSON)),
			WithRegisterServiceHandlers(pb.RegisterGreeterHandler),
		)
		if err != nil {
			panic(err)
		}

		if err := gw.Start(ctx); err != nil {
			panic(err)
		}

	}()
}

func TestGateway(t *testing.T) {
	go runServer()

	client := http.Client{}
	resp, err := client.Get(fmt.Sprintf("http://localhost:9999/hello/%s", "world"))
	if err != nil {
		t.Error(err)
		return
	}

	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Error(err)
		return
	}
	t.Logf("body: %v", string(b))
	var obj pb.HelloReply
	err = json.Unmarshal(b, &obj)
	if err != nil {
		t.Error(err)
		return
	}
	t.Logf("value: %v", obj.String())
}
