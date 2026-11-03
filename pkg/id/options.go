package id

import "time"

// CodeOptions holds the short-code encoding parameters | 持有短码编码参数.
type CodeOptions struct {
	// chars is the alphabet used for encoding | 编码使用的字符表.
	chars []rune
	// Checked construction preserves invalid values for validation; legacy
	// construction keeps ignoring empty alphabets and nonpositive lengths.
	// Checked 构造保留非法值交给校验；legacy 构造继续忽略空字符表与非正长度。
	strict bool
	// n1 is the first multiplier, coprime with len(chars) | 第一个乘数，与字符表长度互素.
	n1 int
	// n2 is the second multiplier, coprime with the code length | 第二个乘数，与码长互素.
	n2 int
	// l is the code length | 码长.
	l int
	// salt is the mixing constant | 混淆用的盐值.
	salt uint64
}

// WithCodeChars sets the alphabet used for code encoding | WithCodeChars 设置编码用的字符表.
func WithCodeChars(arr []rune) func(*CodeOptions) {
	return func(options *CodeOptions) {
		opts := getCodeOptionsOrSetDefault(options)
		if opts.strict || len(arr) > 0 {
			opts.chars = arr
		}
	}
}

// WithCodeN1 sets the first multiplier | WithCodeN1 设置第一个乘数.
func WithCodeN1(n int) func(*CodeOptions) {
	return func(options *CodeOptions) {
		getCodeOptionsOrSetDefault(options).n1 = n
	}
}

// WithCodeN2 sets the second multiplier | WithCodeN2 设置第二个乘数.
func WithCodeN2(n int) func(*CodeOptions) {
	return func(options *CodeOptions) {
		getCodeOptionsOrSetDefault(options).n2 = n
	}
}

// WithCodeL sets the code length | WithCodeL 设置码长.
func WithCodeL(l int) func(*CodeOptions) {
	return func(options *CodeOptions) {
		opts := getCodeOptionsOrSetDefault(options)
		if opts.strict || l > 0 {
			opts.l = l
		}
	}
}

// WithCodeSalt sets the salt | WithCodeSalt 设置盐值.
func WithCodeSalt(salt uint64) func(*CodeOptions) {
	return func(options *CodeOptions) {
		if salt > 0 {
			getCodeOptionsOrSetDefault(options).salt = salt
		}
	}
}

func getCodeOptionsOrSetDefault(options *CodeOptions) *CodeOptions {
	if options == nil {
		return &CodeOptions{
			// base string set, remove 0,1,I,O,U,Z
			// 基础字符表，去掉 0、1、I、O、U、Z
			chars: []rune{
				'2', '3', '4', '5', '6',
				'7', '8', '9', 'A', 'B',
				'C', 'D', 'E', 'F', 'G',
				'H', 'J', 'K', 'L', 'M',
				'N', 'P', 'Q', 'R', 'S',
				'T', 'V', 'W', 'X', 'Y',
			},
			// n1 / len(chars)=30 coprime
			// n1 与 len(chars)=30 互素
			n1: 17,
			// n2 / l coprime
			// n2 与 l 互素
			n2: 5,
			// code length
			// 码长
			l: 8,
			// random number
			// 随机数
			salt: 123567369,
		}
	}
	return options
}

// SonyflakeOptions holds the Sonyflake ID generator parameters | 持有 Sonyflake ID 生成器参数.
type SonyflakeOptions struct {
	// machineId is the instance's machine ID | 本实例的机器号.
	machineId int
	// startTime is the custom epoch | 自定义起始纪元.
	startTime time.Time
	// Preserve all supplied machine IDs for the checked constructor's validation.
	// 保留调用方传入的全部机器号，供 checked 构造器校验。
	strict bool
}

// WithSonyflakeMachineId ignores nonpositive IDs in NewSonyflake for
// compatibility. NewSonyflakeChecked uses the last supplied value, including
// zero and negatives.
// WithSonyflakeMachineId 为兼容在 NewSonyflake 中忽略非正机器号；
// NewSonyflakeChecked 取最后传入的值，包括 0 和负数。
func WithSonyflakeMachineId(id int) func(*SonyflakeOptions) {
	return func(options *SonyflakeOptions) {
		opts := getSonyflakeOptionsOrSetDefault(options)
		if opts.strict || id > 0 {
			opts.machineId = id
		}
	}
}

// WithSonyflakeStartTime sets the custom epoch; the zero time is ignored
// WithSonyflakeStartTime 设置起始时间；零值会被忽略.
func WithSonyflakeStartTime(startTime time.Time) func(*SonyflakeOptions) {
	return func(options *SonyflakeOptions) {
		if !startTime.IsZero() {
			getSonyflakeOptionsOrSetDefault(options).startTime = startTime
		}
	}
}

func getSonyflakeOptionsOrSetDefault(options *SonyflakeOptions) *SonyflakeOptions {
	if options == nil {
		return &SonyflakeOptions{
			machineId: 1,
			startTime: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		}
	}
	return options
}
