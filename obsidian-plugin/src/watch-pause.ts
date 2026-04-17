export interface WatchPauseStatus {
	paused: boolean;
	paused_at?: string;
	reason?: string;
	warning?: string;
}

export interface WatchPauseSummary {
	kind: "active" | "paused" | "offline" | "unknown";
	label: string;
	detail: string;
	actionLabel: string;
	actionDisabled: boolean;
}

type FetchResponse = {
	ok: boolean;
	statusText: string;
	json(): Promise<unknown>;
	text(): Promise<string>;
};

type FetchLike = (input: string, init?: RequestInit) => Promise<FetchResponse>;

function describeHTTPError(action: string, statusText: string, text: string): string {
	const detail = text.trim() || statusText || "request failed";
	return `${action}: ${detail}`;
}

function parseWatchPauseStatus(value: unknown): WatchPauseStatus {
	if (!value || typeof value !== "object") {
		throw new Error("watch status response was not an object");
	}
	const status = value as Record<string, unknown>;
	if (typeof status.paused !== "boolean") {
		throw new Error("watch status response is missing paused");
	}
	if (status.paused_at !== undefined && typeof status.paused_at !== "string") {
		throw new Error("watch status response has invalid paused_at");
	}
	if (status.reason !== undefined && typeof status.reason !== "string") {
		throw new Error("watch status response has invalid reason");
	}
	if (status.warning !== undefined && typeof status.warning !== "string") {
		throw new Error("watch status response has invalid warning");
	}
	return {
		paused: status.paused,
		paused_at: status.paused_at as string | undefined,
		reason: status.reason as string | undefined,
		warning: status.warning as string | undefined,
	};
}

async function requestWatchPause(
	port: number,
	path: string,
	action: string,
	init: RequestInit,
	fetchImpl: FetchLike = fetch,
): Promise<WatchPauseStatus> {
	const resp = await fetchImpl(`http://127.0.0.1:${port}${path}`, init);
	if (!resp.ok) {
		throw new Error(describeHTTPError(action, resp.statusText, await resp.text()));
	}
	return parseWatchPauseStatus(await resp.json());
}

export async function readWatchPauseStatus(port: number, fetchImpl: FetchLike = fetch): Promise<WatchPauseStatus> {
	return requestWatchPause(port, "/watch/status", "watch status", { method: "GET" }, fetchImpl);
}

export async function pauseWatch(port: number, reason: string, fetchImpl: FetchLike = fetch): Promise<WatchPauseStatus> {
	return requestWatchPause(
		port,
		"/watch/pause",
		"watch pause",
		{
			method: "POST",
			headers: { "Content-Type": "application/json" },
			body: JSON.stringify({ reason }),
		},
		fetchImpl,
	);
}

export async function resumeWatch(port: number, fetchImpl: FetchLike = fetch): Promise<WatchPauseStatus> {
	return requestWatchPause(
		port,
		"/watch/resume",
		"watch resume",
		{
			method: "POST",
			headers: { "Content-Type": "application/json" },
		},
		fetchImpl,
	);
}

function pausedDetail(status: WatchPauseStatus): string {
	const parts = [];
	if (status.paused_at) {
		parts.push(`since ${status.paused_at}`);
	}
	if (status.reason) {
		parts.push(status.reason);
	}
	if (status.warning) {
		parts.push(`warning: ${status.warning}`);
	}
	return parts.join(" • ") || "Watcher-managed spawns are paused.";
}

export function summarizeWatchPause(status: WatchPauseStatus | null, error?: string): WatchPauseSummary {
	if (error === "offline") {
		return {
			kind: "offline",
			label: "watch offline",
			detail: "No terminal server running. Start `tickets watch`.",
			actionLabel: "Pause",
			actionDisabled: true,
		};
	}
	if (error) {
		return {
			kind: "unknown",
			label: "watch unknown",
			detail: error,
			actionLabel: status?.paused ? "Resume" : "Pause",
			actionDisabled: true,
		};
	}
	if (status?.paused) {
		return {
			kind: "paused",
			label: "watch paused",
			detail: pausedDetail(status),
			actionLabel: "Resume",
			actionDisabled: false,
		};
	}
	return {
		kind: "active",
		label: "watch active",
		detail: "Watcher-managed spawns are active.",
		actionLabel: "Pause",
		actionDisabled: false,
	};
}
