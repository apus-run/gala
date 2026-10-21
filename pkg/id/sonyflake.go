package id

import (
	"context"
	"fmt"

	"github.com/sony/sonyflake/v2"
)

type Sonyflake struct {
	ops   SonyflakeOptions
	sf    *sonyflake.Sonyflake
	Error error
}

// NewSonyflake constructs a generator using the legacy option semantics:
// WithSonyflakeMachineId ignores nonpositive values. Check Error after construction.
// The application must ensure that active generators have distinct machine IDs.
func NewSonyflake(options ...func(*SonyflakeOptions)) *Sonyflake {
	return newSonyflake(false, options...)
}

func newSonyflake(strict bool, options ...func(*SonyflakeOptions)) *Sonyflake {
	ops := getSonyflakeOptionsOrSetDefault(nil)
	ops.strict = strict
	for _, f := range options {
		f(ops)
	}
	sf := &Sonyflake{
		ops: *ops,
	}
	if ops.machineId < 0 || ops.machineId > 65535 {
		sf.Error = fmt.Errorf("sonyflake: machine ID must be in [0, 65535]")
		return sf
	}
	st := sonyflake.Settings{
		StartTime: ops.startTime,
	}
	if ops.machineId >= 0 {
		st.MachineID = func() (int, error) {
			return ops.machineId, nil
		}
	}

	ins, err := sonyflake.New(st)
	if ins == nil || err != nil {
		// A nil instance must not be probed below; report and stop here.
		sf.Error = fmt.Errorf("create sonyflake: %w", err)
		return sf
	}

	if _, err = ins.NextID(); err != nil {
		sf.Error = fmt.Errorf("invalid start time: %w", err)
	}

	sf.sf = ins

	return sf
}

func (s *Sonyflake) NextID() (uint64, error) {
	if s.Error != nil {
		return 0, s.Error
	}
	id, err := s.sf.NextID()
	if err != nil {
		return 0, fmt.Errorf("sonyflake get id failed: %w", err)
	}
	return uint64(id), nil
}

// Id is the legacy convenience API. Deprecated: use NextIDContext for errors.
func (s *Sonyflake) Id(ctx context.Context) uint64 {
	n, _ := s.NextIDContext(ctx)
	return n
}

// NewSonyflakeChecked validates the final machine ID in [0, 65535] and returns
// configuration errors directly. Repeated machine-ID options use the last value.
func NewSonyflakeChecked(options ...func(*SonyflakeOptions)) (*Sonyflake, error) {
	s := newSonyflake(true, options...)
	if s.Error != nil {
		return nil, s.Error
	}
	return s, nil
}

// NextIDContext checks cancellation before and after generation. The upstream
// sequence-overflow wait cannot be interrupted; a canceled result is discarded.
func (s *Sonyflake) NextIDContext(ctx context.Context) (uint64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	n, err := s.NextID()
	if err != nil {
		return 0, err
	}
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	return n, nil
}
