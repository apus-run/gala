package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
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

func runServer(t *testing.T) string {
	t.Helper()

	ctx := context.Background()
	srv := grpcServer.NewServer(grpcServer.WithAddress("127.0.0.1:0"))
	pb.RegisterGreeterServer(srv, &service{})
	grpcEndpoint, err := srv.Endpoint()
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		if err := srv.Start(ctx); err != nil {
			panic(err)
		}
	}()
	t.Cleanup(func() { _ = srv.Stop(ctx) })

	conn, err := grpc.NewClient(grpcEndpoint.Host, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("Failed to dial core: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	paralusJSON := NewParalusJSON()
	gw, err := NewServer(
		ctx,
		WithAddress("127.0.0.1:0"),
		WithConn(conn),
		WithServeMuxOpts(runtime.WithMarshalerOption(jsonContentType, paralusJSON)),
		WithRegisterServiceHandlers(pb.RegisterGreeterHandler),
	)
	if err != nil {
		t.Fatal(err)
	}
	gatewayEndpoint, err := gw.Endpoint()
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		if err := gw.Start(ctx); err != nil {
			panic(err)
		}
	}()
	t.Cleanup(func() { _ = gw.Stop(ctx) })

	return gatewayEndpoint.String()
}

func TestGateway(t *testing.T) {
	baseURL := runServer(t)

	client := http.Client{}
	resp, err := client.Get(fmt.Sprintf("%s/hello/%s", baseURL, "world"))
	if err != nil {
		t.Error(err)
		return
	}
	defer resp.Body.Close()

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

func TestWithShutdownFuncNilIsNoop(t *testing.T) {
	opts := Apply(WithShutdownFunc(nil))
	opts.shutdownFunc()
}
