export interface BoardViewState {
	showArchived: boolean;
<<<<<<< HEAD
	filterQuery: string;
=======
	query: string;
>>>>>>> tickets/TIC-135
}

export function readBoardViewState(state: unknown): BoardViewState {
	const showArchived =
		typeof state === "object" &&
		state !== null &&
		"showArchived" in state &&
		(state as { showArchived?: unknown }).showArchived === true;
	const filterQuery =
		typeof state === "object" &&
		state !== null &&
		"filterQuery" in state &&
		typeof (state as { filterQuery?: unknown }).filterQuery === "string"
			? (state as { filterQuery: string }).filterQuery
			: "";

<<<<<<< HEAD
	return { showArchived, filterQuery };
=======
	const query =
		typeof state === "object" &&
		state !== null &&
		"query" in state &&
		typeof (state as { query?: unknown }).query === "string"
			? (state as { query: string }).query
			: "";

	return { showArchived, query };
>>>>>>> tickets/TIC-135
}
