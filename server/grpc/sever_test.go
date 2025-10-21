package grpc

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	pb "github.com/apus-run/gala/server/internal/testdata/helloworld"
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

type testKey struct{}

func TestNewServer(t *testing.T) {
	ctx := context.Background()
	ctx = context.WithValue(ctx, testKey{}, "test")
	addr := "0.0.0.0:9090"
	srv := NewServer(
		WithAddress(addr),
		WithGrpcOptions(grpc.InitialConnWindowSize(0)),
	)

	// Attach the Greeter service to the core
	pb.RegisterGreeterServer(srv, &service{})

	if e, err := srv.Endpoint(); err != nil || e == nil || strings.HasSuffix(e.Host, ":0") {
		t.Fatal(e, err)
	}

	go func() {
		// start server
		if err := srv.Start(ctx); err != nil {
			panic(err)
		}
	}()
	time.Sleep(time.Second)
	testClient(t, srv)
	_ = srv.Stop(ctx)
}

func testClient(t *testing.T, srv *Server) {
	addr, err := srv.Endpoint()
	if err != nil {
		t.Fatal(err)
	}

	conn, err := grpc.NewClient(addr.Host, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		panic(err)
	}

	defer func() {
		_ = conn.Close()
	}()

	client := pb.NewGreeterClient(conn)
	reply, err := client.SayHello(context.Background(), &pb.HelloRequest{Name: "gaea"})
	t.Log(err)
	if err != nil {
		t.Errorf("failed to call: %v", err)
	}
	if !reflect.DeepEqual(reply.Message, "Hello gaea") {
		t.Errorf("expect %s, got %s", "Hello gaea", reply.Message)
	}

	streamCli, err := client.SayHelloStream(context.Background())
	if err != nil {
		t.Error(err)
		return
	}
	defer func() {
		_ = streamCli.CloseSend()
	}()
	err = streamCli.Send(&pb.HelloRequest{Name: "cc"})
	if err != nil {
		t.Error(err)
		return
	}
	reply, err = streamCli.Recv()
	if err != nil {
		t.Error(err)
		return
	}
	if !reflect.DeepEqual(reply.Message, "hello cc") {
		t.Errorf("expect %s, got %s", "hello cc", reply.Message)
	}

}
