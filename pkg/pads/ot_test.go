package pads

import (
	"encoding/json"
	"math/rand"
	"testing"
)

const alphabet = "abcdefgh \n😀ç"

func randText(r *rand.Rand, n int) Text {
	rs := []rune(alphabet)
	var s []rune
	for i := 0; i < n; i++ {
		s = append(s, rs[r.Intn(len(rs))])
	}
	return FromString(string(s))
}

// randOp builds a random operation on doc (never splitting a surrogate pair,
// like CodeMirror).
func randOp(r *rand.Rand, doc Text) *Operation {
	op := &Operation{}
	i := 0
	for i < len(doc) {
		n := 1 + r.Intn(3)
		if i+n > len(doc) {
			n = len(doc) - i
		}
		// keep surrogate pairs together
		if end := i + n; end < len(doc) && utf16IsLow(doc[end]) {
			n++
		}
		switch r.Intn(4) {
		case 0:
			op.Delete(n)
		case 1:
			op.Insert(randText(r, 1+r.Intn(3)))
			op.Retain(n)
		default:
			op.Retain(n)
		}
		i += n
	}
	if r.Intn(2) == 0 {
		op.Insert(randText(r, 1+r.Intn(3)))
	}
	return op
}

func utf16IsLow(u uint16) bool { return u >= 0xdc00 && u <= 0xdfff }

func TestTransformConverges(t *testing.T) {
	r := rand.New(rand.NewSource(42))
	for iter := 0; iter < 3000; iter++ {
		doc := randText(r, r.Intn(30))
		a, b := randOp(r, doc), randOp(r, doc)
		ap, bp, err := Transform(a, b)
		if err != nil {
			t.Fatal(err)
		}
		da, _ := a.Apply(doc)
		db, _ := b.Apply(doc)
		left, err1 := bp.Apply(da)
		right, err2 := ap.Apply(db)
		if err1 != nil || err2 != nil || left.String() != right.String() {
			t.Fatalf("diverged on %q: a=%v b=%v -> %q vs %q (%v %v)", doc.String(), js(a), js(b), left.String(), right.String(), err1, err2)
		}
	}
}

func TestJSONRoundTripAndTie(t *testing.T) {
	var op Operation
	if err := json.Unmarshal([]byte(`[3,"xy😀",-2,1]`), &op); err != nil {
		t.Fatal(err)
	}
	if op.BaseLen != 6 || op.TargetLen != 8 {
		t.Fatalf("lengths %d %d", op.BaseLen, op.TargetLen)
	}
	if got := js(&op); got != `[3,"xy😀",-2,1]` {
		t.Fatal(got)
	}
	out, err := op.Apply(FromString("abcdef"))
	if err != nil || out.String() != "abcxy😀f" {
		t.Fatalf("%q %v", out.String(), err)
	}
	// both insert at the same spot: a's text comes first on both sides
	a := (&Operation{}).Insert(FromString("A")).Retain(2)
	b := (&Operation{}).Insert(FromString("B")).Retain(2)
	ap, bp, _ := Transform(a, b)
	l, _ := a.Apply(FromString("xy"))
	l, _ = bp.Apply(l)
	rr, _ := b.Apply(FromString("xy"))
	rr, _ = ap.Apply(rr)
	if l.String() != "ABxy" || rr.String() != "ABxy" {
		t.Fatalf("%q %q", l.String(), rr.String())
	}
	if _, err := (&Operation{}).Retain(3).Apply(FromString("ab")); err == nil {
		t.Fatal("length mismatch accepted")
	}
	for _, bad := range []string{`[0]`, `[""]`, `[true]`, `{}`} {
		var o Operation
		if json.Unmarshal([]byte(bad), &o) == nil {
			t.Fatalf("accepted %s", bad)
		}
	}
}

func js(o *Operation) string { b, _ := json.Marshal(o); return string(b) }
