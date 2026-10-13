package mr_test

import (
	"fmt"

	"github.com/apus-run/gala/pkg/mr"
)

func ExampleMapReduce() {
	total, err := mr.MapReduce(
		func(source chan<- int) {
			for i := 1; i <= 100; i++ {
				source <- i
			}
		},
		func(item int, writer mr.Writer[int], _ func(error)) { writer.Write(item * item) },
		func(pipe <-chan int, writer mr.Writer[int], _ func(error)) {
			sum := 0
			for value := range pipe {
				sum += value
			}
			writer.Write(sum)
		}, mr.WithWorkers(8),
	)
	if err != nil {
		panic(err)
	}
	fmt.Println(total)
	// Output: 338350
}
