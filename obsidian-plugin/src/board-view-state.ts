export interface BoardViewState {
	showArchived: boolean;
	filterQuery: string;
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

	return { showArchived, filterQuery };
}
