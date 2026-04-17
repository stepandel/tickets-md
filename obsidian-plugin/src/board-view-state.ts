export interface BoardViewState {
	showArchived: boolean;
	query: string;
}

export function readBoardViewState(state: unknown): BoardViewState {
	const showArchived =
		typeof state === "object" &&
		state !== null &&
		"showArchived" in state &&
		(state as { showArchived?: unknown }).showArchived === true;

	const query =
		typeof state === "object" &&
		state !== null &&
		"query" in state &&
		typeof (state as { query?: unknown }).query === "string"
			? (state as { query: string }).query
			: "";

	return { showArchived, query };
}
