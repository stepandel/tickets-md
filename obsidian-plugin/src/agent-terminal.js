export const ACTIVE_AGENT_STATUSES = new Set(["spawned", "running", "blocked"]);
export const QUEUED_STATUS = "queued";

export function isQueued(ticket) {
	return Boolean(
		ticket?.queued_at
		&& (!ticket?.agent_status || !ACTIVE_AGENT_STATUSES.has(ticket.agent_status)),
	);
}

export function effectiveAgentStatus(ticket) {
	if (ticket?.agent_status && ACTIVE_AGENT_STATUSES.has(ticket.agent_status)) {
		return ticket.agent_status;
	}
	if (isQueued(ticket)) {
		return QUEUED_STATUS;
	}
	return ticket?.agent_status;
}

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
