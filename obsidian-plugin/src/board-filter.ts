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

function parseQueryTerms(normalizedQuery: string): string[] {
	const terms: string[] = [];

	for (let i = 0; i < normalizedQuery.length;) {
		for (i; i < normalizedQuery.length && /\s/.test(normalizedQuery[i]); i++) {
		}
		if (i >= normalizedQuery.length) {
			break;
		}

		if (normalizedQuery[i] === "\"") {
			i++;
			const start = i;
			for (i; i < normalizedQuery.length && normalizedQuery[i] !== "\""; i++) {
			}
			const phrase = normalizedQuery.slice(start, i);
			if (phrase.trim()) {
				terms.push(phrase);
			}
			if (i < normalizedQuery.length && normalizedQuery[i] === "\"") {
				i++;
			}
			continue;
		}

		const start = i;
		for (i; i < normalizedQuery.length && !/\s/.test(normalizedQuery[i]) && normalizedQuery[i] !== "\""; i++) {
		}
		const token = normalizedQuery.slice(start, i);
		if (token) {
			terms.push(token);
		}
	}

	return terms;
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

	const tokens = parseQueryTerms(normalizedQuery);
	if (tokens.length === 0) {
		return true;
	}

	const fields = searchableFields(ticket);
	return tokens.every((token) => fields.some((field) => field.includes(token)));
}
