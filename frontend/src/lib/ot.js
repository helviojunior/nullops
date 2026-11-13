// Operational Transformation for plain text (port of ot.js, MIT), mirrored by
// pkg/pads/ot.go on the server. An operation walks the whole document:
//   n > 0   retain n characters
//   "str"   insert str
//   n < 0   delete -n characters
// Lengths are UTF-16 code units (JavaScript strings, CodeMirror offsets).

const isRetain = (o) => typeof o === "number" && o > 0;
const isDelete = (o) => typeof o === "number" && o < 0;
const isInsert = (o) => typeof o === "string";

export class TextOperation {
  constructor() {
    this.ops = [];
    this.baseLength = 0;
    this.targetLength = 0;
  }

  retain(n) {
    if (n === 0) return this;
    this.baseLength += n;
    this.targetLength += n;
    const last = this.ops.length - 1;
    if (isRetain(this.ops[last])) this.ops[last] += n;
    else this.ops.push(n);
    return this;
  }

  insert(str) {
    if (str === "") return this;
    this.targetLength += str.length;
    const ops = this.ops;
    const last = ops.length - 1;
    if (isInsert(ops[last])) ops[last] += str;
    else if (isDelete(ops[last])) {
      // keep inserts before deletes (canonical form)
      if (isInsert(ops[last - 1])) ops[last - 1] += str;
      else {
        ops[last + 1] = ops[last];
        ops[last] = str;
      }
    } else ops.push(str);
    return this;
  }

  delete(n) {
    if (n === 0) return this;
    if (n > 0) n = -n;
    this.baseLength -= n;
    const last = this.ops.length - 1;
    if (isDelete(this.ops[last])) this.ops[last] += n;
    else this.ops.push(n);
    return this;
  }

  isNoop() {
    return this.ops.length === 0 || (this.ops.length === 1 && isRetain(this.ops[0]));
  }

  apply(str) {
    if (str.length !== this.baseLength) throw new Error("operation does not match the document");
    const out = [];
    let i = 0;
    for (const o of this.ops) {
      if (isRetain(o)) {
        out.push(str.slice(i, i + o));
        i += o;
      } else if (isInsert(o)) out.push(o);
      else i -= o;
    }
    return out.join("");
  }

  // this then other -> one operation
  compose(other) {
    if (this.targetLength !== other.baseLength) throw new Error("compose: length mismatch");
    const op = new TextOperation();
    const ops1 = this.ops, ops2 = other.ops;
    let i1 = 0, i2 = 0;
    let o1 = ops1[i1++], o2 = ops2[i2++];
    for (;;) {
      if (o1 === undefined && o2 === undefined) break;
      if (isDelete(o1)) { op.delete(o1); o1 = ops1[i1++]; continue; }
      if (isInsert(o2)) { op.insert(o2); o2 = ops2[i2++]; continue; }
      if (o1 === undefined || o2 === undefined) throw new Error("compose: too short");
      if (isRetain(o1) && isRetain(o2)) {
        if (o1 > o2) { op.retain(o2); o1 -= o2; o2 = ops2[i2++]; }
        else if (o1 === o2) { op.retain(o1); o1 = ops1[i1++]; o2 = ops2[i2++]; }
        else { op.retain(o1); o2 -= o1; o1 = ops1[i1++]; }
      } else if (isInsert(o1) && isDelete(o2)) {
        if (o1.length > -o2) { o1 = o1.slice(-o2); o2 = ops2[i2++]; }
        else if (o1.length === -o2) { o1 = ops1[i1++]; o2 = ops2[i2++]; }
        else { o2 += o1.length; o1 = ops1[i1++]; }
      } else if (isInsert(o1) && isRetain(o2)) {
        if (o1.length > o2) { op.insert(o1.slice(0, o2)); o1 = o1.slice(o2); o2 = ops2[i2++]; }
        else if (o1.length === o2) { op.insert(o1); o1 = ops1[i1++]; o2 = ops2[i2++]; }
        else { op.insert(o1); o2 -= o1.length; o1 = ops1[i1++]; }
      } else if (isRetain(o1) && isDelete(o2)) {
        if (o1 > -o2) { op.delete(o2); o1 += o2; o2 = ops2[i2++]; }
        else if (o1 === -o2) { op.delete(o2); o1 = ops1[i1++]; o2 = ops2[i2++]; }
        else { op.delete(o1); o2 += o1; o1 = ops1[i1++]; }
      } else throw new Error("compose: invalid operations");
    }
    return op;
  }

  // Concurrent a, b on the same document -> [a', b'] with
  // apply(apply(d, a), b') === apply(apply(d, b), a'). On a tie, a's insert first.
  static transform(a, b) {
    if (a.baseLength !== b.baseLength) throw new Error("transform: length mismatch");
    const ap = new TextOperation(), bp = new TextOperation();
    const ops1 = a.ops, ops2 = b.ops;
    let i1 = 0, i2 = 0;
    let o1 = ops1[i1++], o2 = ops2[i2++];
    for (;;) {
      if (o1 === undefined && o2 === undefined) break;
      if (isInsert(o1)) { ap.insert(o1); bp.retain(o1.length); o1 = ops1[i1++]; continue; }
      if (isInsert(o2)) { ap.retain(o2.length); bp.insert(o2); o2 = ops2[i2++]; continue; }
      if (o1 === undefined || o2 === undefined) throw new Error("transform: too short");
      let min;
      if (isRetain(o1) && isRetain(o2)) {
        if (o1 > o2) { min = o2; o1 -= o2; o2 = ops2[i2++]; }
        else if (o1 === o2) { min = o2; o1 = ops1[i1++]; o2 = ops2[i2++]; }
        else { min = o1; o2 -= o1; o1 = ops1[i1++]; }
        ap.retain(min);
        bp.retain(min);
      } else if (isDelete(o1) && isDelete(o2)) {
        if (-o1 > -o2) { o1 -= o2; o2 = ops2[i2++]; }
        else if (o1 === o2) { o1 = ops1[i1++]; o2 = ops2[i2++]; }
        else { o2 -= o1; o1 = ops1[i1++]; }
      } else if (isDelete(o1) && isRetain(o2)) {
        if (-o1 > o2) { min = o2; o1 += o2; o2 = ops2[i2++]; }
        else if (-o1 === o2) { min = o2; o1 = ops1[i1++]; o2 = ops2[i2++]; }
        else { min = -o1; o2 += o1; o1 = ops1[i1++]; }
        ap.delete(min);
      } else if (isRetain(o1) && isDelete(o2)) {
        if (o1 > -o2) { min = -o2; o1 += o2; o2 = ops2[i2++]; }
        else if (o1 === -o2) { min = o1; o1 = ops1[i1++]; o2 = ops2[i2++]; }
        else { min = o1; o2 += o1; o1 = ops1[i1++]; }
        bp.delete(min);
      } else throw new Error("transform: invalid operations");
    }
    return [ap, bp];
  }

  toJSON() {
    return this.ops;
  }

  static fromJSON(ops) {
    const op = new TextOperation();
    for (const o of ops) {
      if (isRetain(o)) op.retain(o);
      else if (isInsert(o)) op.insert(o);
      else if (isDelete(o)) op.delete(o);
      else throw new Error("invalid operation component");
    }
    return op;
  }

  // CodeMirror ChangeSet (relative to a document of docLength) -> operation.
  static fromChanges(changes, docLength) {
    const op = new TextOperation();
    let pos = 0;
    changes.iterChanges((fromA, toA, _fromB, _toB, inserted) => {
      op.retain(fromA - pos);
      op.insert(inserted.toString());
      op.delete(toA - fromA);
      pos = toA;
    });
    op.retain(docLength - pos);
    return op;
  }

  // operation -> CodeMirror change spec (positions in the current document).
  toChanges() {
    const changes = [];
    let pos = 0;
    for (const o of this.ops) {
      if (isRetain(o)) pos += o;
      else if (isInsert(o)) changes.push({ from: pos, insert: o });
      else {
        changes.push({ from: pos, to: pos - o });
        pos -= o;
      }
    }
    return changes;
  }
}
