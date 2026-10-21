package s3

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"testing/iotest"

	"github.com/apus-run/gala/components/fileserver"
	"github.com/aws/aws-sdk-go-v2/aws"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"
)

// Exercise the public Read path so stream wrappers can change independently.
func TestReadStreamLengthAndErrors(t *testing.T) {
	transportErr := errors.New("transport failed")
	for _, tc := range []struct {
		name   string
		length *int64
		end    error
		want   error
	}{
		{"unknown length", nil, io.EOF, nil},
		{"unknown truncated", nil, io.ErrUnexpectedEOF, io.ErrUnexpectedEOF},
		{"exact length", aws.Int64(3), io.EOF, nil},
		{"short body", aws.Int64(4), io.EOF, io.ErrUnexpectedEOF},
		{"short unexpected EOF", aws.Int64(4), io.ErrUnexpectedEOF, io.ErrUnexpectedEOF},
		{"complete unexpected EOF", aws.Int64(3), io.ErrUnexpectedEOF, nil},
		{"transport error", aws.Int64(3), transportErr, transportErr},
	} {
		t.Run(tc.name, func(t *testing.T) {
			api := &streamAPI{length: tc.length, body: io.MultiReader(strings.NewReader("abc"), iotest.ErrReader(tc.end))}
			s, err := New(api, Config{Bucket: "b"})
			if err != nil {
				t.Fatal(err)
			}
			rc, err := s.Read(t.Context(), "key")
			if err != nil {
				t.Fatal(err)
			}
			defer rc.Close()
			data, err := io.ReadAll(rc)
			if string(data) != "abc" || !errors.Is(err, tc.want) {
				t.Fatalf("Read = %q, %v; want abc, %v", data, err, tc.want)
			}
			if tc.want != nil {
				var detail *fileserver.Error
				if !errors.As(err, &detail) || detail.Op != "Read" || detail.Key != "key" {
					t.Fatalf("missing operation context: %v", err)
				}
			}
		})
	}
}

type streamAPI struct {
	API
	length *int64
	body   io.Reader
}

func (a *streamAPI) GetObject(context.Context, *awss3.GetObjectInput, ...func(*awss3.Options)) (*awss3.GetObjectOutput, error) {
	return &awss3.GetObjectOutput{Body: io.NopCloser(a.body), ContentLength: a.length}, nil
}
