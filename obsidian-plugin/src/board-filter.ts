export interface FilterableBoardTicket {
	id: string;
	title: string;
	priority?: string;
	project?: string;
	labels?: string[];
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

function searchableFields(ticket: FilterableBoardTicket): string[] {
	return [
		ticket.id,
		ticket.title,
		ticket.priority,
		ticket.project,
		...(ticket.labels ?? []),
		ticket.stage,
		ticket.agent_status,
		ticket.parent,
		...(ticket.related ?? []),
		...(ticket.blocked_by ?? []),
		...(ticket.blocks ?? []),
		...(ticket.children ?? []),
	]
		.filter((value): value is string => Boolean(value))
		.map((value) => value.toLowerCase());
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

	const fields = searchableFields(ticket);
	return tokens.every((token) => fields.some((field) => field.includes(token)));
}
