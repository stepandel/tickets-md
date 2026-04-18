export interface FilterableBoardTicket {
	id: string;
	title: string;
	priority?: string;
	project?: string;
	stage?: string;
	agent_status?: string;
	related?: string[];
	blocked_by?: string[];
	blocks?: string[];
	parent?: string;
	children?: string[];
}

export function normalizeBoardFilterQuery(query: string): string {
	return query.trim().toLowerCase();
}

export function ticketMatchesBoardFilter(ticket: FilterableBoardTicket, query: string): boolean {
	const normalizedQuery = normalizeBoardFilterQuery(query);
	if (!normalizedQuery) {
		return true;
	}

	const tokens = normalizedQuery.split(/\s+/).filter(Boolean);
	if (tokens.length === 0) {
		return true;
	}

	const fields = [
		ticket.id,
		ticket.title,
		ticket.priority ?? "",
		ticket.project ?? "",
		ticket.stage ?? "",
		ticket.agent_status ?? "",
		ticket.parent ?? "",
		...(ticket.related ?? []),
		...(ticket.blocked_by ?? []),
		...(ticket.blocks ?? []),
		...(ticket.children ?? []),
	].map((value) => value.toLowerCase());

	return tokens.every((token) => fields.some((value) => value.includes(token)));
}
