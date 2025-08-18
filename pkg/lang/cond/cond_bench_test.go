package cond

import (
	"testing"
)

func BenchmarkIf(b *testing.B) {
	cond := true

	b.Run("Baseline", func(b *testing.B) {
		var v int
		for i := 0; i < b.N; i++ {
			if cond {
				v = 1
			} else {
				v = 2
			}
		}
		if v != 1 {
			b.FailNow()
		}
	})

	b.Run("If", func(b *testing.B) {
		var v int
		for i := 0; i < b.N; i++ {
			v = If(cond, 1, 2)
		}
		if v != 1 {
			b.FailNow()
		}
	})

	b.Run("IfLazy", func(b *testing.B) {
		var v int
		for i := 0; i < b.N; i++ {
			v = IfLazy(cond, func() int { return 1 }, func() int { return 2 })
		}
		if v != 1 {
			b.FailNow()
		}
	})
}
