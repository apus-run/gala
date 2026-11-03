package id

import (
	"context"
	"fmt"

	"github.com/sony/sonyflake/v2"
)

// Sonyflake wraps a sonyflake generator built from SonyflakeOptions
// Sonyflake 封装由 SonyflakeOptions 构建的 sonyflake 生成器.
type Sonyflake struct {
	ops SonyflakeOptions
	sf  *sonyflake.Sonyflake
	// Error holds the construction error; check it after NewSonyflake
	// Error 存放构造错误；NewSonyflake 之后需检查.
	Error error
}

// NewSonyflake constructs a generator using the legacy option semantics:
// WithSonyflakeMachineId ignores nonpositive values. Check Error after construction.
// The application must ensure that active generators have distinct machine IDs.
// NewSonyflake 按旧版选项语义构造生成器：WithSonyflakeMachineId 忽略非正值。
// 构造后请检查 Error。应用必须保证同时运行的生成器机器号互不相同。
//
// Warning: without WithSonyflakeMachineId the machine ID is fixed at 1 — two
// replicas both on the default generate identical IDs within the same
// millisecond. Every instance in a multi-replica deployment must set a
// distinct machine ID (e.g. derived from the instance ordinal or the IP).
// The default was not changed to another number: any fixed default has the
// same problem, and changing it would alter the ID sequence of existing
// deployments.
// 警告：不传 WithSonyflakeMachineId 时机器号固定为 1——两个副本都用默认值，
// 同一毫秒内会生成相同的 ID。多实例部署必须给每个实例设置不同的机器号
// （例如从实例序号或 IP 派生）。默认值没有改成别的数：任何固定的默认值
// 都有同样的问题，而改它会让已有部署的 ID 序列发生变化。
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
		// 下面的代码不得探测 nil 实例；在这里报告错误并终止。
		sf.Error = fmt.Errorf("create sonyflake: %w", err)
		return sf
	}

	if _, err = ins.NextID(); err != nil {
		sf.Error = fmt.Errorf("invalid start time: %w", err)
	}

	sf.sf = ins

	return sf
}

// NextID returns the next distributed ID | NextID 返回下一个分布式 ID.
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

// Id is the legacy convenience API.
// Id 是旧版便捷 API。
//
// Deprecated: use NextIDContext for errors.
// Deprecated: 需要错误信息请用 NextIDContext。
func (s *Sonyflake) Id(ctx context.Context) uint64 {
	n, _ := s.NextIDContext(ctx)
	return n
}

// NewSonyflakeChecked validates the final machine ID in [0, 65535] and returns
// configuration errors directly. Repeated machine-ID options use the last value.
// NewSonyflakeChecked 校验最终机器号落在 [0, 65535]，并直接返回配置错误；
// 重复传入的机器号取最后一个值。
func NewSonyflakeChecked(options ...func(*SonyflakeOptions)) (*Sonyflake, error) {
	s := newSonyflake(true, options...)
	if s.Error != nil {
		return nil, s.Error
	}
	return s, nil
}

// NextIDContext checks cancellation before and after generation. The upstream
// sequence-overflow wait cannot be interrupted; a canceled result is discarded.
// NextIDContext 在生成前后各检查一次取消。上游的序列溢出等待无法被中断；
// 已取消的结果会被丢弃。
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
