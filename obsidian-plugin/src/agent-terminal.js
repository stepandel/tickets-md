export const ACTIVE_AGENT_STATUSES = new Set(["spawned", "running", "blocked"]);

export function hasLiveTerminal(ticket) {
	return Boolean(ticket?.agent_session);
}

export function cronHasLiveSession(run) {
	return Boolean(run?.session && run?.status && ACTIVE_AGENT_STATUSES.has(run.status));
}

export function canReplayTerminal(ticket) {
	return Boolean(
		ticket?.agent_run
		&& ticket?.agent_status
		&& !ACTIVE_AGENT_STATUSES.has(ticket.agent_status),
	);
}

export function ticketRunLogPath(ticketId, runId) {
	return `.agents/${ticketId}/runs/${runId}.log`;
}
