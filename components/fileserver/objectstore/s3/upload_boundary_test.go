package s3

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"testing"
)

// A reader may return its final byte together with a non-EOF failure.
// The lookahead byte must not turn that failure into a successful upload.
type failedTailReader struct {
	err  error
	done bool
}

func (r *failedTailReader) Read(p []byte) (int, error) {
	if r.done {
		return 0, io.EOF
	}
	r.done = true
	p[0] = 'x'
	return 1, r.err
}

func TestUploadRejectsLookaheadReadFailure(t *testing.T) {
	boom := errors.New("source failed with a byte")
	api := newMemAPI()
	s := newTestStore(t, api, Config{PartSize: MinPartSize, Concurrency: 2})
	if err := s.Upload(t.Context(), "obj", bytes.NewReader([]byte("old"))); err != nil {
		t.Fatal(err)
	}
	input := io.MultiReader(bytes.NewReader(make([]byte, MinPartSize)), &failedTailReader{err: boom})
	if err := s.Upload(t.Context(), "obj", input); !errors.Is(err, boom) {
		t.Fatalf("Upload = %v, want source failure", err)
	}
	api.mu.Lock()
	defer api.mu.Unlock()
	if string(api.objects["obj"].data) != "old" || api.creates != 0 {
		t.Fatal("failed lookahead changed the object or began multipart upload")
	}
}

// emptyUntilEOF terminates even if the driver's progress guard is removed, so
// the regression fails by assertion rather than hanging the test process.
type emptyUntilEOF struct{ calls int }

func (r *emptyUntilEOF) Read([]byte) (int, error) {
	r.calls++
	if r.calls > 200 {
		return 0, io.EOF
	}
	return 0, nil
}

type dataWithError struct {
	data []byte
	err  error
}

func (r *dataWithError) Read(p []byte) (int, error) {
	if len(r.data) == 0 {
		return 0, io.EOF
	}
	n := copy(p, r.data)
	r.data = r.data[n:]
	if len(r.data) == 0 {
		return n, r.err
	}
	return n, nil
}

func TestUploadRejectsReadFailuresInsidePart(t *testing.T) {
	boom := errors.New("input failed at exact buffer boundary")
	for _, tc := range []struct {
		name  string
		input io.Reader
		want  error
	}{
		{"empty reads", &emptyUntilEOF{}, io.ErrNoProgress},
		{"unexpected EOF", &dataWithError{data: []byte("partial"), err: io.ErrUnexpectedEOF}, io.ErrUnexpectedEOF},
		{"full buffer with error", &dataWithError{data: make([]byte, MinPartSize), err: boom}, boom},
	} {
		t.Run(tc.name, func(t *testing.T) {
			api := newMemAPI()
			s := newTestStore(t, api, Config{PartSize: MinPartSize})
			err := s.Upload(t.Context(), "obj", tc.input)
			if !errors.Is(err, tc.want) {
				t.Fatalf("Upload = %v, want %v", err, tc.want)
			}
			api.mu.Lock()
			defer api.mu.Unlock()
			if api.puts != 0 || api.creates != 0 {
				t.Fatal("invalid input reached the backend")
			}
		})
	}
}

func TestUploadAcceptsDataWithEOFAtPartBoundaries(t *testing.T) {
	for _, size := range []int{0, 1, int(MinPartSize) - 1, int(MinPartSize), int(MinPartSize) + 1, 2 * int(MinPartSize)} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			api := newMemAPI()
			s := newTestStore(t, api, Config{PartSize: MinPartSize, Concurrency: 2})
			data := bytes.Repeat([]byte("x"), size)
			if err := s.Upload(t.Context(), "obj", &dataWithError{data: data, err: io.EOF}); err != nil {
				t.Fatal(err)
			}
			api.mu.Lock()
			defer api.mu.Unlock()
			if !bytes.Equal(api.objects["obj"].data, data) {
				t.Fatal("uploaded bytes differ")
			}
		})
	}
}
