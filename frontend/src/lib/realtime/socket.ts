/*
 * Single shared WebSocket connection to /api/v1/ws.
 *
 * Why one connection: the backend hub multiplexes — UI components
 * subscribe to typed topics and the same socket carries everything.
 * Browsers limit WS-per-origin and we'd waste handshake cost otherwise.
 *
 * API for callers:
 *   subscribe(topic, cb): unsubscribe()
 *
 * Auto-reconnect with exponential backoff (1s → 30s). Subscriptions are
 * remembered and re-sent after each reconnect. Drops over the wire are
 * surfaced via a "_meta" topic message; callers can listen to that
 * topic if they care about gap detection.
 */

type Listener = (data: unknown) => void;

interface ServerMessage {
	topic: string;
	data?: unknown;
}

class Socket {
	private ws: WebSocket | null = null;
	private listeners = new Map<string, Set<Listener>>();
	private connecting = false;
	private closed = false;
	private backoff = 1000;
	private readonly url: string;

	constructor(url: string) {
		this.url = url;
	}

	subscribe(topic: string, cb: Listener): () => void {
		let set = this.listeners.get(topic);
		if (!set) {
			set = new Set();
			this.listeners.set(topic, set);
			this.send({ op: 'subscribe', topic });
		}
		set.add(cb);
		this.ensureOpen();
		return () => this.unsubscribe(topic, cb);
	}

	private unsubscribe(topic: string, cb: Listener) {
		const set = this.listeners.get(topic);
		if (!set) return;
		set.delete(cb);
		if (set.size === 0) {
			this.listeners.delete(topic);
			this.send({ op: 'unsubscribe', topic });
		}
	}

	close() {
		this.closed = true;
		this.ws?.close();
		this.ws = null;
	}

	private ensureOpen() {
		if (this.closed) return;
		if (this.ws && (this.ws.readyState === WebSocket.OPEN || this.ws.readyState === WebSocket.CONNECTING)) return;
		if (this.connecting) return;
		this.connecting = true;

		const ws = new WebSocket(this.url);
		this.ws = ws;

		ws.addEventListener('open', () => {
			this.connecting = false;
			this.backoff = 1000;
			// Re-send all current subscriptions after a reconnect.
			for (const topic of this.listeners.keys()) {
				ws.send(JSON.stringify({ op: 'subscribe', topic }));
			}
		});

		ws.addEventListener('message', (e) => {
			let msg: ServerMessage;
			try {
				msg = JSON.parse(typeof e.data === 'string' ? e.data : '');
			} catch {
				return;
			}
			const set = this.listeners.get(msg.topic);
			if (!set) return;
			for (const cb of set) {
				try {
					cb(msg.data);
				} catch (err) {
					console.error('realtime listener threw for topic', msg.topic, err);
				}
			}
		});

		ws.addEventListener('close', () => {
			this.ws = null;
			this.connecting = false;
			if (this.closed || this.listeners.size === 0) return;
			const delay = this.backoff;
			this.backoff = Math.min(this.backoff * 2, 30_000);
			setTimeout(() => this.ensureOpen(), delay);
		});

		ws.addEventListener('error', () => {
			// 'close' will fire next; reconnect logic lives there.
		});
	}

	private send(msg: object) {
		if (!this.ws || this.ws.readyState !== WebSocket.OPEN) return;
		this.ws.send(JSON.stringify(msg));
	}
}

let singleton: Socket | null = null;

/**
 * Subscribe to a realtime topic. Returns an unsubscribe function.
 * Lazy-opens the underlying socket on first subscription; idempotent
 * across components.
 */
export function subscribe(topic: string, cb: (data: unknown) => void): () => void {
	if (typeof window === 'undefined') {
		return () => {};
	}
	if (!singleton) {
		const proto = window.location.protocol === 'https:' ? 'wss' : 'ws';
		const url = `${proto}://${window.location.host}/api/v1/ws`;
		singleton = new Socket(url);
	}
	return singleton.subscribe(topic, cb);
}

/** For tests only — close the singleton between scenarios. */
export function _resetSocketForTests() {
	singleton?.close();
	singleton = null;
}
