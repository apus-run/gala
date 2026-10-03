package client_test

import (
	"fmt"
	"testing"
	"time"

	client "example.com/option-pattern"
)

func withFastFail() client.Option {
	return client.OptionFunc(func(o *client.Options) {
		o.Apply(client.WithTimeout(time.Second), client.WithRetries(0))
	})
}

func TestExternalComposition(t *testing.T) {
	c, err := client.New(withFastFail())
	if err != nil {
		t.Fatal(err)
	}
	opts := c.Options()
	if opts.Timeout() != time.Second || opts.Retries() != 0 {
		t.Fatalf("external composition: timeout=%v retries=%d", opts.Timeout(), opts.Retries())
	}
}

func ExampleNew() {
	c, err := client.New(
		client.WithTimeout(2*time.Second),
		client.WithRetries(0),
	)
	if err != nil {
		fmt.Println(err)
		return
	}
	opts := c.Options()
	fmt.Println(opts.Timeout())
	fmt.Println(opts.Retries())
	// Output:
	// 2s
	// 0
}
