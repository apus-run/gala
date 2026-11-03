// Package id encodes numeric IDs as short codes and generates Sonyflake
// distributed IDs | 把数字 ID 编码为短码，并生成 Sonyflake 分布式 ID.
//
// # Notes | 说明
//
// Two independent generators. NewCodeChecked encodes a uint64 injectively
// within [0, len(chars)^length) using exact big.Int arithmetic; it validates
// the alphabet (no duplicate runes) and requires n1 coprime to the alphabet
// size and n2 coprime to the length. It is an encoding, not encryption. The
// legacy NewCode is deprecated: its output is not injective (inputs past the
// code space collide) and out-of-range options yield an empty string. The
// Sonyflake wrapper defaults the machine ID to 1, so every replica must set a
// distinct one or IDs repeat within the same millisecond. NewSonyflake
// reports setup failures in the Error field; NewSonyflakeChecked returns
// them; NextIDContext checks cancellation before and after generation but
// cannot interrupt the upstream sequence-overflow wait.
// 包内有两个独立生成器。NewCodeChecked 用 big.Int 精确算术把 uint64 单射
// 编码到 [0, len(chars)^length)；会校验字符表（无重复字符），要求 n1 与
// 字符表大小互素、n2 与长度互素。它是编码不是加密。旧版 NewCode 已弃用：
// 输出不单射（超出码空间即碰撞），非法参数返回空串。Sonyflake 封装的机器
// 号默认 1，多副本必须各自设置不同机器号，否则同一毫秒会生成相同 ID。
// NewSonyflake 把构造错误放在 Error 字段；NewSonyflakeChecked 直接返回错误；
// NextIDContext 在生成前后检查取消，但无法中断上游序列溢出等待。
//
// # Features | 功能
//
// - NewCodeChecked: injective short codes with validated options | 单射短码，参数可校验
// - Sonyflake IDs via NextID and NextIDContext | Sonyflake ID 与感知取消的 NextIDContext
// - Options for alphabet, length, salt, machine ID, start time | 字符表/长度/盐/机器号/起始时间选项
//
// # Example | 示例
//
//	code, err := id.NewCodeChecked(42) // 8 chars from the default alphabet
//	sf, err := id.NewSonyflakeChecked(id.WithSonyflakeMachineId(2))
//	n, err := sf.NextID()
package id
