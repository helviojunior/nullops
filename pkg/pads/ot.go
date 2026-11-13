// Package pads implements PastePad: shared plain-text pads edited by several
// users at the same time.
//
// Concurrent edits are merged with Operational Transformation (OT). A text
// operation walks the whole document and is a list of components:
//
//	retain n  (n > 0)   keep n characters
//	insert s  (string)  insert s
//	delete n  (n < 0)   delete -n characters
//
// Lengths are counted in UTF-16 code units, like JavaScript strings and the
// CodeMirror editor, so the server and the browser always agree on offsets.
// The algorithms are a Go port of ot.js (MIT), mirrored in
// frontend/src/lib/ot.js.
package pads

import (
	"encoding/json"
	"errors"
	"fmt"
	"unicode/utf16"
)

// Text is a document as UTF-16 code units.
type Text []uint16

// FromString converts a Go string.
func FromString(s string) Text { return utf16.Encode([]rune(s)) }

// String converts back to a Go string.
func (t Text) String() string { return string(utf16.Decode(t)) }

// component of an operation: exactly one of Retain (>0), Delete (>0) or
// Insert (non-empty) is set.
type component struct {
	Retain int
	Delete int
	Insert Text
}

// Operation is a text operation.
type Operation struct {
	ops       []component
	BaseLen   int // length of the document it applies to
	TargetLen int // length after applying it
}

// Retain keeps n characters.
func (o *Operation) Retain(n int) *Operation {
	if n <= 0 {
		return o
	}
	o.BaseLen += n
	o.TargetLen += n
	if l := len(o.ops); l > 0 && o.ops[l-1].Retain > 0 {
		o.ops[l-1].Retain += n
	} else {
		o.ops = append(o.ops, component{Retain: n})
	}
	return o
}

// Insert inserts s. Inserts are kept before an adjacent delete (canonical form).
func (o *Operation) Insert(s Text) *Operation {
	if len(s) == 0 {
		return o
	}
	o.TargetLen += len(s)
	l := len(o.ops)
	switch {
	case l > 0 && o.ops[l-1].Insert != nil:
		o.ops[l-1].Insert = append(append(Text{}, o.ops[l-1].Insert...), s...)
	case l > 0 && o.ops[l-1].Delete > 0:
		if l > 1 && o.ops[l-2].Insert != nil {
			o.ops[l-2].Insert = append(append(Text{}, o.ops[l-2].Insert...), s...)
		} else {
			o.ops = append(o.ops, o.ops[l-1])
			o.ops[l-1] = component{Insert: append(Text{}, s...)}
		}
	default:
		o.ops = append(o.ops, component{Insert: append(Text{}, s...)})
	}
	return o
}

// Delete deletes n characters.
func (o *Operation) Delete(n int) *Operation {
	if n <= 0 {
		return o
	}
	o.BaseLen += n
	if l := len(o.ops); l > 0 && o.ops[l-1].Delete > 0 {
		o.ops[l-1].Delete += n
	} else {
		o.ops = append(o.ops, component{Delete: n})
	}
	return o
}

// IsNoop reports whether the operation changes nothing.
func (o *Operation) IsNoop() bool {
	return len(o.ops) == 0 || (len(o.ops) == 1 && o.ops[0].Retain > 0)
}

// ErrMismatch is returned when an operation does not fit the document.
var ErrMismatch = errors.New("operation does not match the document")

// Apply applies the operation to doc.
func (o *Operation) Apply(doc Text) (Text, error) {
	if len(doc) != o.BaseLen {
		return nil, ErrMismatch
	}
	out := make(Text, 0, o.TargetLen)
	i := 0
	for _, c := range o.ops {
		switch {
		case c.Retain > 0:
			if i+c.Retain > len(doc) {
				return nil, ErrMismatch
			}
			out = append(out, doc[i:i+c.Retain]...)
			i += c.Retain
		case c.Insert != nil:
			out = append(out, c.Insert...)
		default:
			i += c.Delete
		}
	}
	if i != len(doc) {
		return nil, ErrMismatch
	}
	return out, nil
}

// Transform takes two operations a and b made concurrently on the same
// document and returns a', b' such that apply(apply(d, a), b') ==
// apply(apply(d, b), a'). On a tie (both insert at the same place) a's
// insert comes first.
func Transform(a, b *Operation) (*Operation, *Operation, error) {
	if a.BaseLen != b.BaseLen {
		return nil, nil, ErrMismatch
	}
	ap, bp := &Operation{}, &Operation{}
	ops1, ops2 := a.ops, b.ops
	i1, i2 := 0, 0
	var o1, o2 *component
	next1 := func() {
		if i1 < len(ops1) {
			c := ops1[i1]
			o1 = &c
			i1++
		} else {
			o1 = nil
		}
	}
	next2 := func() {
		if i2 < len(ops2) {
			c := ops2[i2]
			o2 = &c
			i2++
		} else {
			o2 = nil
		}
	}
	next1()
	next2()
	for o1 != nil || o2 != nil {
		if o1 != nil && o1.Insert != nil {
			ap.Insert(o1.Insert)
			bp.Retain(len(o1.Insert))
			next1()
			continue
		}
		if o2 != nil && o2.Insert != nil {
			ap.Retain(len(o2.Insert))
			bp.Insert(o2.Insert)
			next2()
			continue
		}
		if o1 == nil || o2 == nil {
			return nil, nil, ErrMismatch
		}
		switch {
		case o1.Retain > 0 && o2.Retain > 0:
			var m int
			switch {
			case o1.Retain > o2.Retain:
				m = o2.Retain
				o1.Retain -= o2.Retain
				next2()
			case o1.Retain == o2.Retain:
				m = o2.Retain
				next1()
				next2()
			default:
				m = o1.Retain
				o2.Retain -= o1.Retain
				next1()
			}
			ap.Retain(m)
			bp.Retain(m)
		case o1.Delete > 0 && o2.Delete > 0:
			switch {
			case o1.Delete > o2.Delete:
				o1.Delete -= o2.Delete
				next2()
			case o1.Delete == o2.Delete:
				next1()
				next2()
			default:
				o2.Delete -= o1.Delete
				next1()
			}
		case o1.Delete > 0 && o2.Retain > 0:
			var m int
			switch {
			case o1.Delete > o2.Retain:
				m = o2.Retain
				o1.Delete -= o2.Retain
				next2()
			case o1.Delete == o2.Retain:
				m = o2.Retain
				next1()
				next2()
			default:
				m = o1.Delete
				o2.Retain -= o1.Delete
				next1()
			}
			ap.Delete(m)
		case o1.Retain > 0 && o2.Delete > 0:
			var m int
			switch {
			case o1.Retain > o2.Delete:
				m = o2.Delete
				o1.Retain -= o2.Delete
				next2()
			case o1.Retain == o2.Delete:
				m = o1.Retain
				next1()
				next2()
			default:
				m = o1.Retain
				o2.Delete -= o1.Retain
				next1()
			}
			bp.Delete(m)
		default:
			return nil, nil, ErrMismatch
		}
	}
	return ap, bp, nil
}

// MarshalJSON encodes as the ot.js wire format: [5, "abc", -2].
func (o *Operation) MarshalJSON() ([]byte, error) {
	out := make([]any, 0, len(o.ops))
	for _, c := range o.ops {
		switch {
		case c.Retain > 0:
			out = append(out, c.Retain)
		case c.Insert != nil:
			out = append(out, c.Insert.String())
		default:
			out = append(out, -c.Delete)
		}
	}
	return json.Marshal(out)
}

// UnmarshalJSON decodes the ot.js wire format.
func (o *Operation) UnmarshalJSON(b []byte) error {
	var raw []json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	*o = Operation{}
	for _, r := range raw {
		var s string
		if err := json.Unmarshal(r, &s); err == nil {
			if s == "" {
				return errors.New("empty insert")
			}
			o.Insert(FromString(s))
			continue
		}
		var n int
		if err := json.Unmarshal(r, &n); err != nil || n == 0 {
			return fmt.Errorf("invalid operation component %s", r)
		}
		if n > 0 {
			o.Retain(n)
		} else {
			o.Delete(-n)
		}
	}
	return nil
}
