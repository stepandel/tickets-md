export function formatForceRerunDescription(ticketId: string, sessionId?: string): string {
	const sessionLabel = sessionId ? ` (${sessionId})` : "";
	return `Forcing a re-run will terminate the active agent session for ${ticketId}${sessionLabel} and start a new run. Progress in the running session will be lost.`;
}
