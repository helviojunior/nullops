import { TextOperation } from "./ot";
import { ApiError } from "./api";

const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
const randomId = () =>
  Array.from(crypto.getRandomValues(new Uint8Array(8)), (b) => b.toString(16).padStart(2, "0")).join("");

/**
 * Collaborative editing client for one pad (ot.js client state machine):
 *   synchronized -> (local edit) -> awaiting confirm(outstanding)
 *   awaiting     -> (local edit) -> awaiting with buffer(outstanding, buffer)
 * Server changes arrive in order through a long poll; our own changes in
 * that stream acknowledge the outstanding operation.
 *
 * call(action, payload) performs an encrypted API call.
 * hooks: applyRemote(op), resync(text), status(state), editors(list), error(err)
 */
export class PadClient {
  constructor(name, call, hooks) {
    this.name = name;
    this.call = call;
    this.hooks = hooks;
    this.client = randomId();
    this.seq = 0;
    this.rev = 0;
    this.outstanding = null;
    this.buffer = null;
    this.running = false;
  }

  async open() {
    const snap = await this.call("pad_open", { name: this.name, client: this.client });
    this.rev = snap.rev;
    this.hooks.editors?.(snap.editors || []);
    return snap.text;
  }

  start() {
    this.running = true;
    this.pollLoop();
  }

  stop() {
    this.running = false;
  }

  get pending() {
    return !!this.outstanding;
  }

  // A local edit (already applied to the editor).
  local(op) {
    if (op.isNoop()) return;
    if (!this.outstanding) {
      this.outstanding = op;
      this.send(op, this.rev);
    } else if (!this.buffer) {
      this.buffer = op;
    } else {
      this.buffer = this.buffer.compose(op);
    }
    this.hooks.status?.("saving");
  }

  async send(op, rev) {
    const seq = ++this.seq;
    for (let attempt = 0; this.running || attempt === 0; attempt++) {
      try {
        // retries reuse seq: the server applies an operation only once
        await this.call("pad_edit", { name: this.name, client: this.client, seq, rev, op: op.toJSON() });
        return;
      } catch (err) {
        if (err instanceof ApiError && err.status === 401) return this.hooks.error?.(err);
        if (err instanceof ApiError && err.status === 200) {
          // rejected (resync / too large / deleted): reload from the server
          return this.reload(err);
        }
        this.hooks.status?.("offline");
        await sleep(Math.min(8000, 500 * 2 ** attempt));
      }
    }
  }

  serverAck() {
    if (!this.outstanding) return;
    if (this.buffer) {
      this.outstanding = this.buffer;
      this.buffer = null;
      this.send(this.outstanding, this.rev);
    } else {
      this.outstanding = null;
      this.hooks.status?.("synced");
    }
  }

  serverOp(op) {
    if (this.outstanding) {
      const [o1, op1] = TextOperation.transform(this.outstanding, op);
      this.outstanding = o1;
      op = op1;
      if (this.buffer) {
        const [b1, op2] = TextOperation.transform(this.buffer, op);
        this.buffer = b1;
        op = op2;
      }
    }
    this.hooks.applyRemote(op);
  }

  async reload(err) {
    this.outstanding = null;
    this.buffer = null;
    try {
      const text = await this.open();
      this.hooks.resync(text, err);
      this.hooks.status?.("synced");
    } catch (e) {
      this.hooks.error?.(e);
    }
  }

  async pollLoop() {
    let failures = 0;
    while (this.running) {
      try {
        const res = await this.call("pad_poll", { name: this.name, client: this.client, rev: this.rev });
        if (!this.running) return;
        failures = 0;
        if (res.snapshot) {
          this.outstanding = null;
          this.buffer = null;
          this.rev = res.snapshot.rev;
          this.hooks.resync(res.snapshot.text, null);
        } else {
          for (const ch of res.changes) {
            this.rev++;
            if (ch.client === this.client) this.serverAck();
            else this.serverOp(TextOperation.fromJSON(ch.op));
          }
          this.rev = res.rev;
        }
        this.hooks.editors?.(res.editors || []);
        if (!this.outstanding) this.hooks.status?.("synced");
      } catch (err) {
        if (!this.running) return;
        if (err instanceof ApiError && (err.status === 401 || err.message === "Pad not found")) {
          this.running = false;
          return this.hooks.error?.(err);
        }
        failures++;
        this.hooks.status?.("offline");
        await sleep(Math.min(10000, 500 * 2 ** failures));
      }
    }
  }
}
