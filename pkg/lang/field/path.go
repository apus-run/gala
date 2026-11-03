// Package field represents the path from a root object to a particular field,
// usable as map keys in error paths.
// 包 field 表示从根对象到某个字段的路径，可用作错误路径中的定位。
package field

import (
	"bytes"
	"fmt"
	"strconv"
)

// Path represents the path from some root to a particular field.
// Path 表示从某个根到特定字段的路径。
type Path struct {
	name   string // the name of this field or "" if this is an index | 字段名；若为下标则为 ""
	index  string // if name == "", this is a subscript (index or map key) of the previous element | name 为 "" 时，表示前一个元素的下标（索引或 map key）
	parent *Path  // nil if this is the root element | 为根元素时是 nil
}

// NewPath creates a root Path object.
// NewPath 创建一个根 Path 对象。
func NewPath(name string, moreNames ...string) *Path {
	r := &Path{name: name, parent: nil}
	for _, anotherName := range moreNames {
		r = &Path{name: anotherName, parent: r}
	}
	return r
}

// Root returns the root element of this Path.
// Root 返回该 Path 的根元素。
func (p *Path) Root() *Path {
	for ; p.parent != nil; p = p.parent {
		// Do nothing.
		// 什么都不做，只为走到根。
	}
	return p
}

// Child creates a new Path that is a child of the method receiver.
// Child 创建一个作为当前 Path 子节点的新 Path。
func (p *Path) Child(name string, moreNames ...string) *Path {
	r := NewPath(name, moreNames...)
	r.Root().parent = p
	return r
}

// Index indicates that the previous Path is to be subscripted by an int.
// This sets the same underlying value as Key.
// Index 表示前一个 Path 用 int 下标访问，与 Key 设置的是同一个底层值。
func (p *Path) Index(index int) *Path {
	return &Path{index: strconv.Itoa(index), parent: p}
}

// Key indicates that the previous Path is to be subscripted by a string.
// This sets the same underlying value as Index.
// Key 表示前一个 Path 用字符串下标访问，与 Index 设置的是同一个底层值。
func (p *Path) Key(key string) *Path {
	return &Path{index: key, parent: p}
}

// String produces a string representation of the Path.
// String 生成该 Path 的字符串表示。
func (p *Path) String() string {
	// make a slice to iterate
	// 构造一个切片用于迭代
	elems := []*Path{}
	for ; p != nil; p = p.parent {
		elems = append(elems, p)
	}

	// iterate, but it has to be backwards
	// 倒序迭代
	buf := bytes.NewBuffer(nil)
	for i := range elems {
		p := elems[len(elems)-1-i]
		if p.parent != nil && len(p.name) > 0 {
			// This is either the root or it is a subscript.
			// 这里要么是根节点，要么是下标节点。
			buf.WriteString(".")
		}
		if len(p.name) > 0 {
			buf.WriteString(p.name)
		} else {
			fmt.Fprintf(buf, "[%s]", p.index)
		}
	}
	return buf.String()
}
