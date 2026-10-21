# Id


id generator.


## Code


generate short str by unique uint64.


### Usage


```bash
go get -u github.com/apus-run/gala/pkg/id
```

```go
import (
	"fmt"
    "github.com/apus-run/gala/pkg/id"
)

func main() {
	fmt.Println(id.NewCode(1))
	fmt.Println(id.NewCode(2))
	fmt.Println(id.NewCode(3))
	fmt.Println(id.NewCode(4))

	fmt.Println(id.NewCode(
		1,
		id.WithCodeChars([]rune{'0', '1', '2', '3', '4', '5', '6', '7', '8', '9'}),
		id.WithCodeN1(9),
		id.WithCodeN2(3),
		id.WithCodeL(5),
		id.WithCodeSalt(99999),
	))
}
```


### Options


- `WithCodeChars` - code set, each char will generate from this set, default ['2', '3', '4', '5', '6', '7', '8', '9', 'A', 'B', 'C', 'D', 'E', 'F', 'G', 'H', 'J', 'K', 'L', 'M', 'N', 'P', 'Q', 'R', 'S', 'T', 'V', 'W', 'X', 'Y']
- `WithCodeL` - code length
- `WithCodeN1` - n1 & chars.length coprime
- `WithCodeN2` - n2 & code.length coprime
- `WithCodeSalt` - code salt, same options and same uint64 id will generate same code, u can set different salt to generate new code


## Snowflake Id


generate snowflake id based on [sonyflake](https://github.com/sony/sonyflake).


### Usage


```go
import (
	"context"
	"fmt"

    "github.com/apus-run/gala/pkg/id"
)

func main() {
	sf := id.NewSonyflake(
		id.WithSonyflakeMachineId(1),
	)
	if sf.Error != nil {
		fmt.Println(sf.Error)
		return
	}
	fmt.Println(sf.Id(context.Background()))
}
```


### Options


- `WithSonyflakeMachineId` - machine id; legacy construction ignores nonpositive values (see below)
- `WithSonyflakeStartTime` - start time, do not modify after setting once, otherwise, u may get duplicate ids

## Checked APIs

Prefer `NewSonyflakeChecked(opts...) (*Sonyflake, error)` and
`NextIDContext(ctx) (uint64, error)` for new callers. The checked constructor
validates the final machine ID in [0, 65535]: zero is valid, negative values and
values above 65535 return an error and a nil generator. Repeated
`WithSonyflakeMachineId` options use the last supplied value; a later valid value
can replace an earlier invalid one.

Legacy `NewSonyflake` keeps ignoring nonpositive `WithSonyflakeMachineId` values,
preserving the default machine ID 1 or a previously supplied positive value.
For example, options `(7), (0)` use machine ID 7 in the legacy constructor and
machine ID 0 in the checked constructor; `(7), (-1)` keep 7 in legacy construction
but fail checked construction. Positive values above 65535 still fail if they
are not replaced by a later applicable option; legacy reports failures via Error.
Both constructors default to machine ID 1 when no machine-ID option is supplied.
The application must keep machine IDs unique among active generators in the same
ID domain. Constructors preserve underlying errors. Existing `NewSonyflake`,
`Error`, `NextID`, and `Id` remain available; `Id` returns 0 on failure and is
deprecated in favor of the explicit-error method. It no longer retries permanent
errors forever. Cancellation is checked before and after generation; an upstream
sequence/clock wait cannot be interrupted.

`NewCodeChecked(value, opts...) (string, error)` validates distinct alphabet
characters, length (1–255), and coprime permutation multipliers. Explicit empty
alphabets and nonpositive lengths return an error instead of using defaults.
Repeated options use the last supplied value, with one compatibility exception:
`WithCodeSalt(0)` is ignored by both `NewCodeChecked` and `NewCode`, preserving
the default salt or the previously supplied positive salt. For example,
`WithCodeSalt(7), WithCodeSalt(0)` keeps salt 7. Checked validation applies to
the final configuration. Legacy `NewCode` still ignores empty alphabets and
nonpositive lengths.
The valid input range is `[0, alphabetSize^length)`: the default 8-character alphabet supports
IDs below 656100000000, **not arbitrary 64-bit Snowflake IDs**. It uses exact
arithmetic to prevent intermediate overflow. Outputs match legacy `NewCode`
where its arithmetic does not overflow; unusual configurations that previously
wrapped byte arithmetic can produce different results. Legacy `NewCode` remains
unchanged. Neither encoding is encryption or a secret/token generator.
