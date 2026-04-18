import { WatchPauseStatus } from "./watch-pause";

export interface WatchServerInfo {
	port: number;
	pid: number;
}

export interface WatchStateSnapshot {
	status: WatchPauseStatus | null;
	error: string | null;
	port: number | null;
}

export async function loadWatchStateSnapshot(
	readServer: () => Promise<WatchServerInfo | null>,
	readStatus: (port: number) => Promise<WatchPauseStatus>,
): Promise<WatchStateSnapshot> {
	const serverInfo = await readServer();
	if (!serverInfo) {
		return {
			status: null,
			error: "offline",
			port: null,
		};
	}

	try {
		return {
			status: await readStatus(serverInfo.port),
			error: null,
			port: serverInfo.port,
		};
	} catch (error) {
		return {
			status: null,
			error: error instanceof Error ? error.message : String(error),
			port: serverInfo.port,
		};
	}
}
