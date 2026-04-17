interface FilterableTicket {
	id?: string;
	title?: string;
}

export function matchesFilter(ticket: FilterableTicket, query: string): boolean {
	const normalizedQuery = query.trim().toLocaleLowerCase();
	if (!normalizedQuery) {
		return true;
	}

	const id = (ticket.id ?? "").toLocaleLowerCase();
	const title = (ticket.title ?? "").toLocaleLowerCase();
	return id.includes(normalizedQuery) || title.includes(normalizedQuery);
}
