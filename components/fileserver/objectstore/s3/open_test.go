package s3

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/apus-run/gala/components/fileserver"
	"github.com/aws/aws-sdk-go-v2/aws"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"
)

func TestOpenReturnsGetAttributesWithoutHead(t *testing.T) {
	a := newMemAPI()
	s := newTestStore(t, a, Config{Prefix: "tenant"})
	if err := s.Upload(t.Context(), "k", strings.NewReader("payload"), fileserver.WithContentType("text/plain"), fileserver.WithMetadata(map[string]string{"Color": "blue"})); err != nil {
		t.Fatal(err)
	}
	r, err := s.Open(t.Context(), "k")
	if err != nil {
		t.Fatal(err)
	}
	defer r.Body.Close()
	if a.gets != 1 || a.heads != 0 || r.Info.Key != "k" || r.Info.ContentType != "text/plain" || r.Info.Metadata["color"] != "blue" || r.Info.ETag == "" || r.Info.ModTime.IsZero() {
		t.Fatalf("result=%+v gets=%d heads=%d", r, a.gets, a.heads)
	}
	r.Info.Metadata["color"] = "red"
	info, err := s.Stat(t.Context(), "k")
	if err != nil {
		t.Fatal(err)
	}
	if info.Metadata["color"] != "blue" {
		t.Fatal("Open leaked mutable storage metadata")
	}
}

func TestOpenRejectsReplacementBetweenHeadAndGet(t *testing.T) {
	a := newMemAPI()
	s := newTestStore(t, a, Config{})
	if err := s.Upload(t.Context(), "k", strings.NewReader("old content")); err != nil {
		t.Fatal(err)
	}
	a.getsBeforeSwap = 0
	if _, err := s.Open(t.Context(), "k", fileserver.WithRange(1, 3)); !errors.Is(err, fileserver.ErrConflict) {
		t.Fatalf("Open error=%v", err)
	}
}

type openResponseAPI struct {
	API
	body   io.ReadCloser
	length *int64
}

func (a openResponseAPI) GetObject(context.Context, *awss3.GetObjectInput, ...func(*awss3.Options)) (*awss3.GetObjectOutput, error) {
	return &awss3.GetObjectOutput{Body: a.body, ContentLength: a.length}, nil
}

type closeObserved struct {
	io.Reader
	closed bool
}

func (b *closeObserved) Close() error { b.closed = true; return nil }

func TestOpenRequiresSizeAndClosesRejectedResponse(t *testing.T) {
	body := &closeObserved{Reader: strings.NewReader("payload")}
	s, err := New(openResponseAPI{body: body}, Config{Bucket: "bucket"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Open(t.Context(), "k"); !errors.Is(err, fileserver.ErrUnsupported) || !body.closed {
		t.Fatalf("error=%v closed=%v", err, body.closed)
	}
}

func TestOpenStreamErrorsIdentifyOpen(t *testing.T) {
	s, err := New(openResponseAPI{body: io.NopCloser(strings.NewReader("ab")), length: aws.Int64(3)}, Config{Bucket: "bucket"})
	if err != nil {
		t.Fatal(err)
	}
	r, err := s.Open(t.Context(), "k")
	if err != nil {
		t.Fatal(err)
	}
	defer r.Body.Close()
	_, err = io.ReadAll(r.Body)
	var detail *fileserver.Error
	if !errors.Is(err, io.ErrUnexpectedEOF) || !errors.As(err, &detail) || detail.Op != "Open" {
		t.Fatalf("stream error=%v", err)
	}
}
