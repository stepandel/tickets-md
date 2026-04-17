export interface VisibleStagesConfig {
	stages: string[];
	archive_stage?: string;
}

export function visibleStages(config: VisibleStagesConfig, showArchived = false): string[] {
	if (showArchived || !config.archive_stage) {
		return [...config.stages];
	}
	return config.stages.filter((stage) => stage !== config.archive_stage);
}
