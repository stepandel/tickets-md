export interface BoardViewState {
	showArchived: boolean;
	filterQuery: string;
}

export function readBoardViewState(state: unknown): BoardViewState {
	const stateObject = typeof state === "object" && state !== null ? state as {
		showArchived?: unknown;
		filterQuery?: unknown;
		query?: unknown;
	} : null;

	const showArchived =
		stateObject !== null &&
		"showArchived" in stateObject &&
		stateObject.showArchived === true;

	const filterQuery =
		typeof stateObject?.filterQuery === "string"
			? stateObject.filterQuery
			: typeof stateObject?.query === "string"
				? stateObject.query
				: "";

	return { showArchived, filterQuery };
}
