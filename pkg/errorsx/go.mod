module github.com/apus-run/gala/pkg/errorsx

go 1.25

require (
	google.golang.org/genproto/googleapis/rpc v0.0.0-20251020155222-88f65dc88635
	google.golang.org/grpc v1.76.0
)

require (
	golang.org/x/sys v0.34.0 // indirect
	google.golang.org/protobuf v1.36.10 // indirect
)

replace github.com/apus-run/gala/pkg/errorsx => ../errorsx
