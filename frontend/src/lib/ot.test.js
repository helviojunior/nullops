import { describe, expect, it } from "vitest";
import { TextOperation } from "./ot";

const ALPHA = ["a", "b", "c", " ", "\n", "ç", "😀"];
let seed = 7;
const rnd = (n) => {
  seed = (seed * 1103515245 + 12345) & 0x7fffffff;
  return seed % n;
};
const randStr = (n) => Array.from({ length: n }, () => ALPHA[rnd(ALPHA.length)]).join("");

function randOp(doc) {
  const op = new TextOperation();
  let i = 0;
  while (i < doc.length) {
    let n = Math.min(1 + rnd(3), doc.length - i);
    const c = doc.charCodeAt(i + n);
    if (c >= 0xdc00 && c <= 0xdfff) n++; // keep surrogate pairs together
    const k = rnd(4);
    if (k === 0) op.delete(n);
    else if (k === 1) op.insert(randStr(1 + rnd(3))).retain(n);
    else op.retain(n);
    i += n;
  }
  if (rnd(2)) op.insert(randStr(1 + rnd(3)));
  return op;
}

describe("TextOperation", () => {
  it("transform converges (randomized)", () => {
    for (let k = 0; k < 2000; k++) {
      const doc = randStr(rnd(25));
      const a = randOp(doc), b = randOp(doc);
      const [ap, bp] = TextOperation.transform(a, b);
      expect(bp.apply(a.apply(doc))).toBe(ap.apply(b.apply(doc)));
    }
  });

  it("compose equals sequential application", () => {
    for (let k = 0; k < 1000; k++) {
      const doc = randStr(rnd(25));
      const a = randOp(doc);
      const b = randOp(a.apply(doc));
      expect(a.compose(b).apply(doc)).toBe(b.apply(a.apply(doc)));
    }
  });

  it("speaks the server wire format", () => {
    const op = TextOperation.fromJSON([3, "xy😀", -2, 1]);
    expect(op.apply("abcdef")).toBe("abcxy😀f");
    expect(JSON.stringify(op)).toBe('[3,"xy😀",-2,1]');
    expect(op.toChanges()).toEqual([{ from: 3, insert: "xy😀" }, { from: 3, to: 5 }]);
  });
});

describe("PadClient state machine", () => {
  it("two clients and a server converge with concurrent edits", async () => {
    // a minimal in-memory server with the same rules as pkg/pads
    const server = { text: "", history: [] };
    const edit = (rev, op) => {
      for (const h of server.history.slice(rev)) op = TextOperation.transform(op, h.op)[0];
      server.text = op.apply(server.text);
      server.history.push({ op });
    };
    const { PadClient } = await import("./padClient");
    const mk = (id) => {
      const st = { text: "" };
      const c = new PadClient("p", async () => ({}), {
        applyRemote: (op) => (st.text = op.apply(st.text)),
        resync: () => {},
      });
      c.client = id;
      c.sent = [];
      c.send = (op, rev) => c.sent.push({ op, rev });
      return { c, st };
    };
    const A = mk("A"), B = mk("B");
    const typeAt = (x, at, s) => {
      const op = new TextOperation().retain(at).insert(s).retain(x.st.text.length - at);
      x.st.text = op.apply(x.st.text);
      x.c.local(op);
      x.c.flush(); // the debounce is covered by its own test below
    };
    // deliver queued sends to the server, then stream history to both clients
    const flush = () => {
      for (const x of [A, B]) {
        while (x.c.sent.length) {
          const { op, rev } = x.c.sent.shift();
          edit(rev, op);
          server.history[server.history.length - 1].client = x.c.client;
        }
      }
      for (const x of [A, B]) {
        while (x.c.rev < server.history.length) {
          const h = server.history[x.c.rev++];
          if (h.client === x.c.client) x.c.serverAck();
          else x.c.serverOp(h.op);
        }
      }
    };
    typeAt(A, 0, "hello");
    flush();
    typeAt(A, 5, " world");
    typeAt(B, 0, ">> ");
    typeAt(A, 11, "!"); // buffered behind the outstanding op
    for (let i = 0; i < 4; i++) flush();
    expect(server.text).toBe(">> hello world!");
    expect(A.st.text).toBe(server.text);
    expect(B.st.text).toBe(server.text);
  });
});

describe("PadClient debounce", () => {
  it("sends one operation after a pause instead of one per key, merging remote edits meanwhile", async () => {
    const { PadClient } = await import("./padClient");
    let text = "abc";
    const sent = [];
    const c = new PadClient("p", async () => ({}), {
      applyRemote: (op) => (text = op.apply(text)),
      resync: () => {},
    });
    c.debounceMs = 40;
    c.send = (op, rev) => sent.push({ op, rev });
    const type = (s) => {
      const op = new TextOperation().retain(text.length).insert(s);
      text = op.apply(text);
      c.local(op);
    };
    type("1");
    type("2");
    // a remote edit arrives while ours are still buffered
    c.serverOp(new TextOperation().insert(">").retain(3));
    c.rev = 1;
    type("3");
    expect(sent.length).toBe(0);
    await new Promise((r) => setTimeout(r, 80));
    expect(sent.length).toBe(1);
    expect(sent[0].rev).toBe(1);
    // the single buffered operation applies on the server's text
    expect(sent[0].op.apply(">abc")).toBe(">abc123");
    expect(text).toBe(">abc123");
    // flush() sends immediately (Ctrl+S / leaving the page)
    c.serverAck();
    type("4");
    c.flush();
    expect(sent.length).toBe(2);
  });
});
