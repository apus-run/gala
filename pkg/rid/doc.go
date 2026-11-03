// Package rid builds prefixed, machine-salted resource identifiers | 生成带
// 前缀、按机器加盐的资源标识符.
//
// # Notes | 说明
//
// NewResourceID(prefix).New(counter) returns "prefix-xxxxxx": six characters
// from a 62-letter alphabet, encoded from the counter by the deprecated
// id.NewCode with a salt derived from the machine identity (FNV-1a over
// /etc/machine-id, the DMI product UUID, the hostname, or random bytes as a
// last resort). Uniqueness relies on distinct counters per machine: the
// encoding is not injective past its capacity, collisions are possible, so
// treat the value as a short display ID rather than a primary key.
// NewResourceID(prefix).New(counter) 返回 "prefix-xxxxxx"：从 62 字符表中
// 取 6 个字符，由已弃用的 id.NewCode 对 counter 编码，盐取自机器标识
// （依次尝试 /etc/machine-id、DMI product UUID、主机名，最后回退随机字节，
// 做 FNV-1a）。唯一性依赖各机器 counter 不重复：编码超出容量后不单射、
// 可能碰撞，请把它当短展示 ID，不要当主键。
//
// # Features | 功能
//
// - ResourceID string type with String | ResourceID 字符串类型及 String
// - Salt/ReadMachineID expose the machine identity | 暴露机器标识与盐
//
// # Example | 示例
//
//	r := rid.NewResourceID("order")
//	r.New(42) // "order-" plus 6 characters
package rid
