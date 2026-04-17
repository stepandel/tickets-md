<<<<<<< HEAD
export interface FilterableBoardTicket {
	id: string;
	title: string;
	priority?: string;
	project?: string;
}

export function normalizeBoardFilterQuery(query: string): string {
	return query.trim().toLowerCase();
}

export function ticketMatchesBoardFilter(ticket: FilterableBoardTicket, query: string): boolean {
	const normalizedQuery = normalizeBoardFilterQuery(query);
=======
interface FilterableTicket {
	id?: string;
	title?: string;
}

export function matchesFilter(ticket: FilterableTicket, query: string): boolean {
	const normalizedQuery = query.trim().toLocaleLowerCase();
>>>>>>> tickets/TIC-135
	if (!normalizedQuery) {
		return true;
	}

<<<<<<< HEAD
	return [ticket.id, ticket.title, ticket.priority ?? "", ticket.project ?? ""]
		.some((value) => value.toLowerCase().includes(normalizedQuery));
=======
	const id = (ticket.id ?? "").toLocaleLowerCase();
	const title = (ticket.title ?? "").toLocaleLowerCase();
	return id.includes(normalizedQuery) || title.includes(normalizedQuery);
>>>>>>> tickets/TIC-135
}
