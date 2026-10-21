package id

import "time"

type CodeOptions struct {
	chars []rune
	// Checked construction preserves invalid values for validation; legacy
	// construction keeps ignoring empty alphabets and nonpositive lengths.
	strict bool
	n1     int
	n2     int
	l      int
	salt   uint64
}

func WithCodeChars(arr []rune) func(*CodeOptions) {
	return func(options *CodeOptions) {
		opts := getCodeOptionsOrSetDefault(options)
		if opts.strict || len(arr) > 0 {
			opts.chars = arr
		}
	}
}

func WithCodeN1(n int) func(*CodeOptions) {
	return func(options *CodeOptions) {
		getCodeOptionsOrSetDefault(options).n1 = n
	}
}

func WithCodeN2(n int) func(*CodeOptions) {
	return func(options *CodeOptions) {
		getCodeOptionsOrSetDefault(options).n2 = n
	}
}

func WithCodeL(l int) func(*CodeOptions) {
	return func(options *CodeOptions) {
		opts := getCodeOptionsOrSetDefault(options)
		if opts.strict || l > 0 {
			opts.l = l
		}
	}
}

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
			chars: []rune{
				'2', '3', '4', '5', '6',
				'7', '8', '9', 'A', 'B',
				'C', 'D', 'E', 'F', 'G',
				'H', 'J', 'K', 'L', 'M',
				'N', 'P', 'Q', 'R', 'S',
				'T', 'V', 'W', 'X', 'Y',
			},
			// n1 / len(chars)=30 cop rime
			n1: 17,
			// n2 / l cop rime
			n2: 5,
			// code length
			l: 8,
			// random number
			salt: 123567369,
		}
	}
	return options
}

type SonyflakeOptions struct {
	machineId int
	startTime time.Time
	// Preserve all supplied machine IDs for the checked constructor's validation.
	strict bool
}

// WithSonyflakeMachineId ignores nonpositive IDs in NewSonyflake for compatibility.
// NewSonyflakeChecked uses the last supplied value, including zero and negatives.
func WithSonyflakeMachineId(id int) func(*SonyflakeOptions) {
	return func(options *SonyflakeOptions) {
		opts := getSonyflakeOptionsOrSetDefault(options)
		if opts.strict || id > 0 {
			opts.machineId = id
		}
	}
}

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
