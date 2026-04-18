export interface FilterableBoardTicket {
	id: string;
	title: string;
	priority?: string;
	project?: string;
	stage?: string;
	agent_status?: string;
}

export function normalizeBoardFilterQuery(query: string): string {
	return query.trim().toLowerCase();
}

export function ticketMatchesBoardFilter(ticket: FilterableBoardTicket, query: string): boolean {
	const normalizedQuery = normalizeBoardFilterQuery(query);
	if (!normalizedQuery) {
		return true;
	}

	return [
		ticket.id,
		ticket.title,
		ticket.priority ?? "",
		ticket.project ?? "",
		ticket.stage ?? "",
		ticket.agent_status ?? "",
	]
		.some((value) => value.toLowerCase().includes(normalizedQuery));
}
