export interface BoardViewState {
	showArchived: boolean;
}

export function readBoardViewState(state: unknown): BoardViewState {
	const showArchived =
		typeof state === "object" &&
		state !== null &&
		"showArchived" in state &&
		(state as { showArchived?: unknown }).showArchived === true;

	return { showArchived };
}
