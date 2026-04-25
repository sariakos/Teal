// Dialog store. Promise-returning imperative API used to replace native
// confirm()/prompt() throughout the app:
//
//   if (await dialog.confirm({ title: 'Delete app?', tone: 'danger' })) ...
//   const v = await dialog.prompt({ title: 'Type slug to confirm', expect: 'foo' })
//
// Returns a boolean for confirm(), and the entered string (or null on
// cancel) for prompt(). Only one dialog is shown at a time — successive
// calls queue and are presented in FIFO order.

export type DialogTone = 'default' | 'danger' | 'warning';

interface BaseRequest {
	title: string;
	body?: string;
	tone?: DialogTone;
	confirmLabel?: string;
	cancelLabel?: string;
}

export interface ConfirmRequest extends BaseRequest {
	kind: 'confirm';
}

export interface PromptRequest extends BaseRequest {
	kind: 'prompt';
	placeholder?: string;
	// If set, the user must type this exact value before the confirm
	// button enables. Used for delete-app/delete-volume-style guards.
	expect?: string;
	// Optional initial value.
	initial?: string;
	// Help text rendered under the input — e.g. "This cannot be undone".
	help?: string;
}

interface QueuedDialog {
	id: number;
	request: ConfirmRequest | PromptRequest;
	resolve: (value: boolean | string | null) => void;
}

class DialogStore {
	queue = $state<QueuedDialog[]>([]);
	private nextId = 1;

	get current(): QueuedDialog | null {
		return this.queue[0] ?? null;
	}

	confirm(req: Omit<ConfirmRequest, 'kind'>): Promise<boolean> {
		return new Promise((resolve) => {
			this.queue = [
				...this.queue,
				{
					id: this.nextId++,
					request: { ...req, kind: 'confirm' },
					resolve: (v) => resolve(v === true)
				}
			];
		});
	}

	prompt(req: Omit<PromptRequest, 'kind'>): Promise<string | null> {
		return new Promise((resolve) => {
			this.queue = [
				...this.queue,
				{
					id: this.nextId++,
					request: { ...req, kind: 'prompt' },
					resolve: (v) => resolve(typeof v === 'string' ? v : null)
				}
			];
		});
	}

	resolve(id: number, value: boolean | string | null) {
		const head = this.queue[0];
		if (!head || head.id !== id) return;
		head.resolve(value);
		this.queue = this.queue.slice(1);
	}
}

export const dialog = new DialogStore();
