export interface LabelConfig {
	color: string;
	bold?: boolean;
	order?: number;
}

export interface LabelBadgeStyle {
	backgroundColor: string;
	color: string;
	fontWeight: string;
}

const UNKNOWN_LABEL_COLOR = "#6b7280";

export function normalizeLabelName(name?: string): string {
	return (name ?? "").trim().toLowerCase();
}

export function lookupLabel(
	labels: Record<string, LabelConfig> | undefined,
	name?: string,
): LabelConfig | null {
	const normalized = normalizeLabelName(name);
	if (!normalized || !labels) return null;
	for (const [key, label] of Object.entries(labels)) {
		if (normalizeLabelName(key) === normalized) {
			return label;
		}
	}
	return null;
}

export function labelBadgeStyle(
	labels: Record<string, LabelConfig> | undefined,
	name?: string,
): LabelBadgeStyle {
	const label = lookupLabel(labels, name);
	const backgroundColor = label?.color ?? UNKNOWN_LABEL_COLOR;
	return {
		backgroundColor,
		color: idealTextColor(backgroundColor),
		fontWeight: label?.bold ? "700" : "600",
	};
}

export function orderedLabelNames(labels: Record<string, LabelConfig> | undefined): string[] {
	if (labels == null) {
		return [];
	}

	type Entry = {
		name: string;
		normalized: string;
		order?: number;
	};

	const entries: Entry[] = Object.entries(labels).map(([name, label]) => ({
		name,
		normalized: normalizeLabelName(name),
		order: label.order,
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
	if (!rgb) return "#FFFFFF";
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
