export interface PriorityConfig {
	color: string;
	bold?: boolean;
	order?: number;
}

export interface PriorityBadgeStyle {
	backgroundColor: string;
	color: string;
	fontWeight: string;
}

const DEFAULT_PRIORITIES: Record<string, PriorityConfig> = {
	critical: { color: "#FF5F5F", bold: true },
	urgent: { color: "#FF5F5F", bold: true },
	high: { color: "#FF8C00", bold: true },
	medium: { color: "#FFD700" },
	med: { color: "#FFD700" },
	low: { color: "#888888" },
};

const UNKNOWN_PRIORITY_COLOR = "#FFD700";

export function normalizePriorityName(name?: string): string {
	return (name ?? "").trim().toLowerCase();
}

export function lookupPriority(
	priorities: Record<string, PriorityConfig> | undefined,
	name?: string,
): PriorityConfig | null {
	const normalized = normalizePriorityName(name);
	if (!normalized) return null;
	if (priorities) {
		for (const [key, priority] of Object.entries(priorities)) {
			if (normalizePriorityName(key) === normalized) {
				return priority;
			}
		}
		return null;
	}
	return DEFAULT_PRIORITIES[normalized] ?? null;
}

export function priorityBadgeStyle(
	priorities: Record<string, PriorityConfig> | undefined,
	name?: string,
): PriorityBadgeStyle {
	const priority = lookupPriority(priorities, name);
	const backgroundColor = priority?.color ?? UNKNOWN_PRIORITY_COLOR;
	return {
		backgroundColor,
		color: idealTextColor(backgroundColor),
		fontWeight: priority?.bold ? "700" : "600",
	};
}

export function orderedPriorityNames(
	priorities: Record<string, PriorityConfig> | undefined,
): string[] {
	if (priorities === undefined) {
		return ["critical", "high", "medium", "low"];
	}

	type Entry = {
		name: string;
		normalized: string;
		order?: number;
	};

	const entries: Entry[] = Object.entries(priorities).map(([name, priority]) => ({
		name,
		normalized: normalizePriorityName(name),
		order: priority.order,
	}));

	entries.sort((left, right) => {
		switch (true) {
			case left.order !== undefined && right.order !== undefined:
				if (left.order !== right.order) {
					return left.order - right.order;
				}
				break;
			case left.order !== undefined:
				return -1;
			case right.order !== undefined:
				return 1;
		}
		return left.normalized.localeCompare(right.normalized);
	});

	return entries.map((entry) => entry.name);
}

function idealTextColor(backgroundColor: string): string {
	const rgb = parseHexColor(backgroundColor);
	if (!rgb) return "#111111";
	const luminance = (0.299 * rgb.r + 0.587 * rgb.g + 0.114 * rgb.b) / 255;
	return luminance > 0.6 ? "#111111" : "#FFFFFF";
}

function parseHexColor(color: string): { r: number; g: number; b: number } | null {
	const match = color.trim().match(/^#([0-9a-f]{3}|[0-9a-f]{6})$/i);
	if (!match) return null;
	const hex = match[1];
	if (hex.length === 3) {
		return {
			r: parseInt(hex[0] + hex[0], 16),
			g: parseInt(hex[1] + hex[1], 16),
			b: parseInt(hex[2] + hex[2], 16),
		};
	}
	return {
		r: parseInt(hex.slice(0, 2), 16),
		g: parseInt(hex.slice(2, 4), 16),
		b: parseInt(hex.slice(4, 6), 16),
	};
}
