import { TextOperation } from "./ot";
import { ApiError } from "./api";

const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
const randomId = () =>
  Array.from(crypto.getRandomValues(new Uint8Array(8)), (b) => b.toString(16).padStart(2, "0")).join("");

/**
 * Collaborative editing client for one pad (ot.js client state machine).
 *
 * Local edits are composed into `buffer` and sent only after the user stops
 * typing for `debounceMs` (2 s), so typing does not flood the server with one
 * request per key. At most one operation is in flight (`outstanding`); while it
 * waits for its acknowledgement, new edits keep accumulating in the buffer.
 * Remote operations are transformed against both, so nothing is lost while
 * edits are waiting to be sent. flush() sends right away (Ctrl+S, leaving).
 *
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
    this.debounceMs = 2000;
    this.timer = null;
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

  // Sends what is buffered (best effort) and stops.
  stop() {
    this.flush();
    this.running = false;
  }

  get pending() {
    return !!(this.outstanding || this.buffer);
  }

  // A local edit (already applied to the editor): buffered until the user
  // pauses for debounceMs.
  local(op) {
    if (op.isNoop()) return;
    this.buffer = this.buffer ? this.buffer.compose(op) : op;
    this.hooks.status?.("pending");
    clearTimeout(this.timer);
    this.timer = setTimeout(() => this.flush(), this.debounceMs);
  }

  // Sends the buffered edits now (unless an operation is already in flight:
  // then they go out as soon as it is acknowledged).
  flush() {
    clearTimeout(this.timer);
    this.timer = null;
    if (this.outstanding || !this.buffer) return;
    this.outstanding = this.buffer;
    this.buffer = null;
    this.hooks.status?.("saving");
    this.send(this.outstanding, this.rev);
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
    this.outstanding = null;
    if (!this.buffer) this.hooks.status?.("synced");
    else if (!this.timer) this.flush(); // the user already paused: send now
    else this.hooks.status?.("pending");
  }

  serverOp(op) {
    if (this.outstanding) {
      const [o1, op1] = TextOperation.transform(this.outstanding, op);
      this.outstanding = o1;
      op = op1;
    }
    if (this.buffer) {
      const [b1, op2] = TextOperation.transform(this.buffer, op);
      this.buffer = b1;
      op = op2;
    }
    this.hooks.applyRemote(op);
  }

  async reload(err) {
    clearTimeout(this.timer);
    this.timer = null;
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
          clearTimeout(this.timer);
          this.timer = null;
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
        if (!this.outstanding && !this.buffer) this.hooks.status?.("synced");
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
