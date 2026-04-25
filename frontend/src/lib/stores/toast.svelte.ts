// Toast store. Imperative API used from anywhere:
//   toast.success('Saved')
//   toast.error('Network error', { description: err.message })
//   toast.info('Deploy started', { duration: 4000 })
//
// The <Toaster> component (rendered once in +layout.svelte) reads `items`
// reactively. Items auto-dismiss after `duration` ms unless duration=0
// (sticky). Errors default to longer durations than successes since users
// usually need a moment to absorb the message.

export type ToastLevel = 'success' | 'error' | 'info' | 'warning';

export interface ToastItem {
	id: number;
	level: ToastLevel;
	title: string;
	description?: string;
	duration: number;
	createdAt: number;
}

export interface ToastOptions {
	description?: string;
	duration?: number;
}

const DEFAULT_DURATION: Record<ToastLevel, number> = {
	success: 3500,
	info: 3500,
	warning: 5000,
	error: 6000
};

class ToastStore {
	items = $state<ToastItem[]>([]);
	private nextId = 1;
	private timers = new Map<number, ReturnType<typeof setTimeout>>();

	private push(level: ToastLevel, title: string, opts: ToastOptions = {}) {
		const duration = opts.duration ?? DEFAULT_DURATION[level];
		const item: ToastItem = {
			id: this.nextId++,
			level,
			title,
			description: opts.description,
			duration,
			createdAt: Date.now()
		};
		this.items = [...this.items, item];
		if (duration > 0) {
			const handle = setTimeout(() => this.dismiss(item.id), duration);
			this.timers.set(item.id, handle);
		}
		return item.id;
	}

	success(title: string, opts?: ToastOptions) {
		return this.push('success', title, opts);
	}
	error(title: string, opts?: ToastOptions) {
		return this.push('error', title, opts);
	}
	info(title: string, opts?: ToastOptions) {
		return this.push('info', title, opts);
	}
	warning(title: string, opts?: ToastOptions) {
		return this.push('warning', title, opts);
	}

	dismiss(id: number) {
		const handle = this.timers.get(id);
		if (handle) {
			clearTimeout(handle);
			this.timers.delete(id);
		}
		this.items = this.items.filter((t) => t.id !== id);
	}

	clear() {
		for (const handle of this.timers.values()) clearTimeout(handle);
		this.timers.clear();
		this.items = [];
	}
}

export const toast = new ToastStore();
