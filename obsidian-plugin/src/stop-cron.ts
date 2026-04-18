interface StopCronRunInfo {
	run_id?: string;
	session?: string;
}

export function formatStopCronDescription(
	name: string,
	run: StopCronRunInfo | null | undefined,
): string {
	const details: string[] = [];
	if (run?.run_id) details.push(`run ${run.run_id}`);
	if (run?.session) details.push(`session ${run.session}`);
	const suffix = details.length > 0 ? ` (${details.join(", ")})` : "";
	return `Stopping cron ${name}${suffix} will terminate the live process. The run will be marked failed and this cannot be undone.`;
}
