import {
	Plugin,
	ItemView,
	WorkspaceLeaf,
	TFile,
	TFolder,
	Menu,
	Modal,
	Setting,
	parseYaml,
} from "obsidian";

// ── Types ──────────────────────────────────────────────────────────────

interface TicketsConfig {
	prefix: string;
	stages: string[];
}

interface Ticket {
	id: string;
	title: string;
	priority?: string;
	labels?: string[];
	assignee?: string;
	created_at?: string;
	updated_at?: string;
	file: TFile;
	stage: string;
}

// ── Constants ──────────────────────────────────────────────────────────

const VIEW_TYPE = "tickets-board";
const CONFIG_PATH = "config.yml";

// ── Plugin ─────────────────────────────────────────────────────────────

export default class TicketsBoardPlugin extends Plugin {
	async onload() {
		this.registerView(VIEW_TYPE, (leaf) => new BoardView(leaf));

		this.addRibbonIcon("kanban", "Tickets Board", () => this.activateView());

		this.addCommand({
			id: "open-tickets-board",
			name: "Open Tickets Board",
			callback: () => this.activateView(),
		});
	}

	async activateView() {
		const { workspace } = this.app;

		// Reuse existing leaf if one is open
		let leaf = workspace.getLeavesOfType(VIEW_TYPE)[0];
		if (!leaf) {
			leaf = workspace.getLeaf("tab");
			await leaf.setViewState({ type: VIEW_TYPE, active: true });
		}
		workspace.revealLeaf(leaf);
	}

	onunload() {}
}

// ── Board View ─────────────────────────────────────────────────────────

class BoardView extends ItemView {
	private stages: string[] = [];
	private tickets: Ticket[] = [];
	private previewLeaf: WorkspaceLeaf | null = null;

	getViewType(): string {
		return VIEW_TYPE;
	}

	getDisplayText(): string {
		return "Tickets Board";
	}

	getIcon(): string {
		return "kanban";
	}

	async onOpen() {
		await this.refresh();

		// Re-render when files change (created, deleted, renamed, modified)
		this.registerEvent(this.app.vault.on("create", () => this.refresh()));
		this.registerEvent(this.app.vault.on("delete", () => this.refresh()));
		this.registerEvent(this.app.vault.on("rename", () => this.refresh()));
		this.registerEvent(this.app.vault.on("modify", (f) => {
			// Only refresh if a ticket or config changed
			if (f instanceof TFile && (f.extension === "md" || f.path === CONFIG_PATH)) {
				this.refresh();
			}
		}));
	}

	// ── Data Loading ───────────────────────────────────────────────────

	private async loadConfig(): Promise<TicketsConfig | null> {
		const file = this.app.vault.getAbstractFileByPath(CONFIG_PATH);
		if (!(file instanceof TFile)) return null;
		const raw = await this.app.vault.read(file);
		return parseYaml(raw) as TicketsConfig;
	}

	private async loadTickets(stages: string[]): Promise<Ticket[]> {
		const tickets: Ticket[] = [];

		for (const stage of stages) {
			const folder = this.app.vault.getAbstractFileByPath(stage);
			if (!(folder instanceof TFolder)) continue;

			for (const child of folder.children) {
				if (!(child instanceof TFile) || child.extension !== "md") continue;
				if (child.name.startsWith(".")) continue;

				const ticket = await this.parseTicket(child, stage);
				if (ticket) tickets.push(ticket);
			}
		}

		return tickets;
	}

	private async parseTicket(file: TFile, stage: string): Promise<Ticket | null> {
		const content = await this.app.vault.read(file);
		const match = content.match(/^---\n([\s\S]*?)\n---/);
		if (!match) return null;

		try {
			const fm = parseYaml(match[1]);
			return {
				id: fm.id ?? file.basename,
				title: fm.title ?? file.basename,
				priority: fm.priority,
				labels: fm.labels,
				assignee: fm.assignee,
				created_at: fm.created_at,
				updated_at: fm.updated_at,
				file,
				stage,
			};
		} catch {
			return null;
		}
	}

	// ── Rendering ──────────────────────────────────────────────────────

	private async refresh() {
		const config = await this.loadConfig();
		if (!config) {
			this.contentEl.empty();
			this.contentEl.createEl("p", {
				text: "Could not load config.yml — is this a tickets-md vault?",
				cls: "tb-error",
			});
			return;
		}

		this.stages = config.stages;
		this.tickets = await this.loadTickets(this.stages);
		this.render();
	}

	private render() {
		const container = this.contentEl;
		container.empty();

		// Header
		const header = container.createDiv({ cls: "tb-header" });
		header.createEl("h2", { text: "Tickets Board" });
		const headerActions = header.createDiv({ cls: "tb-header-actions" });

		const refreshBtn = headerActions.createEl("button", {
			cls: "tb-header-btn",
			attr: { "aria-label": "Refresh board" },
		});
		refreshBtn.textContent = "\u21BB";
		refreshBtn.addEventListener("click", () => this.refresh());

		const menuBtn = headerActions.createEl("button", {
			cls: "tb-header-btn",
			attr: { "aria-label": "Board menu" },
		});
		menuBtn.textContent = "\u22EF";
		menuBtn.addEventListener("click", (e) => {
			const menu = new Menu();
			menu.addItem((item) =>
				item.setTitle("Add stage").setIcon("plus").onClick(() => {
					new TextInputModal(
						this.app,
						"New Stage",
						"e.g. testing",
						"",
						(name) => this.createStage(name),
					).open();
				}),
			);
			menu.showAtMouseEvent(e);
		});

		// Board
		const board = container.createDiv({ cls: "tb-board" });

		for (const stage of this.stages) {
			const stageTickets = this.tickets.filter((t) => t.stage === stage);
			this.renderColumn(board, stage, stageTickets);
		}

	}

	private renderColumn(board: HTMLElement, stage: string, tickets: Ticket[]) {
		const column = board.createDiv({ cls: "tb-column" });

		// Column header with right-click context menu
		const colHeader = column.createDiv({ cls: "tb-column-header" });
		colHeader.createEl("span", { text: stage, cls: "tb-stage-name" });
		colHeader.createEl("span", {
			text: String(tickets.length),
			cls: "tb-count",
		});

		colHeader.addEventListener("contextmenu", (e) => {
			e.preventDefault();
			const menu = new Menu();
			menu.addItem((item) =>
				item.setTitle("Rename stage").setIcon("pencil").onClick(() => {
					new TextInputModal(
						this.app,
						"Rename Stage",
						"New name",
						stage,
						(name) => this.renameStage(stage, name),
					).open();
				}),
			);
			menu.addItem((item) =>
				item.setTitle("Edit stage config").setIcon("settings").onClick(() => {
					this.openStageConfig(stage);
				}),
			);
			menu.showAtMouseEvent(e);
		});

		// Drop zone
		const cardList = column.createDiv({ cls: "tb-card-list" });

		cardList.addEventListener("dragover", (e) => {
			e.preventDefault();
			if (e.dataTransfer) e.dataTransfer.dropEffect = "move";
			cardList.addClass("tb-drag-over");
		});

		cardList.addEventListener("dragleave", () => {
			cardList.removeClass("tb-drag-over");
		});

		cardList.addEventListener("drop", async (e) => {
			e.preventDefault();
			cardList.removeClass("tb-drag-over");

			const filePath = e.dataTransfer?.getData("text/plain");
			if (!filePath) return;

			const file = this.app.vault.getAbstractFileByPath(filePath);
			if (!(file instanceof TFile)) return;

			const newPath = `${stage}/${file.name}`;
			if (file.path === newPath) return;

			await this.app.vault.rename(file, newPath);
		});

		// Sort tickets by ID for consistent ordering
		tickets.sort((a, b) => a.id.localeCompare(b.id, undefined, { numeric: true }));

		for (const ticket of tickets) {
			this.renderCard(cardList, ticket);
		}

		// Empty state
		if (tickets.length === 0) {
			cardList.createDiv({ cls: "tb-empty", text: "No tickets" });
		}
	}

	private renderCard(parent: HTMLElement, ticket: Ticket) {
		const card = parent.createDiv({ cls: "tb-card" });
		card.setAttribute("draggable", "true");

		card.addEventListener("dragstart", (e) => {
			if (e.dataTransfer) {
				e.dataTransfer.setData("text/plain", ticket.file.path);
				e.dataTransfer.effectAllowed = "move";
			}
			card.addClass("tb-dragging");
		});

		card.addEventListener("dragend", () => {
			card.removeClass("tb-dragging");
		});

		// Click to open the ticket file in a split to the right
		card.addEventListener("click", async () => {
			// Reuse existing preview leaf if it's still around
			if (!this.previewLeaf || !this.app.workspace.getLeavesOfType("markdown").includes(this.previewLeaf)) {
				this.previewLeaf = this.app.workspace.getLeaf("split");
			}
			await this.previewLeaf.openFile(ticket.file);
		});

		// Card header: ID + priority
		const cardHeader = card.createDiv({ cls: "tb-card-header" });
		cardHeader.createEl("span", { text: ticket.id, cls: "tb-ticket-id" });

		if (ticket.priority) {
			cardHeader.createEl("span", {
				text: ticket.priority,
				cls: `tb-priority tb-priority-${ticket.priority}`,
			});
		}

		// Title
		card.createDiv({ text: ticket.title, cls: "tb-card-title" });

		// Footer: labels + assignee
		const footer = card.createDiv({ cls: "tb-card-footer" });

		if (ticket.labels && ticket.labels.length > 0) {
			const labelsEl = footer.createDiv({ cls: "tb-labels" });
			for (const label of ticket.labels) {
				labelsEl.createEl("span", { text: label, cls: "tb-label" });
			}
		}

		if (ticket.assignee) {
			footer.createEl("span", { text: ticket.assignee, cls: "tb-assignee" });
		}
	}

	// ── Config Writing ─────────────────────────────────────────────────

	private async saveConfig(config: TicketsConfig) {
		const lines = [`prefix: ${config.prefix}`, "stages:"];
		for (const s of config.stages) {
			lines.push(`    - ${s}`);
		}
		const file = this.app.vault.getAbstractFileByPath(CONFIG_PATH);
		if (file instanceof TFile) {
			await this.app.vault.modify(file, lines.join("\n") + "\n");
		}
	}

	// ── Open Stage Config ──────────────────────────────────────────────

	private async openStageConfig(stage: string) {
		const configPath = `${stage}/.stage.yml`;
		let file = this.app.vault.getAbstractFileByPath(configPath);

		if (!(file instanceof TFile)) {
			// Create a starter template
			const template = [
				"agent:",
				"    command: claude",
				'    args: ["--print"]',
				"    prompt: |",
				`        You are the ${stage} agent for {{id}}: "{{title}}".`,
				"        Read {{path}} and follow the instructions.",
				"",
			].join("\n");
			file = await this.app.vault.create(configPath, template);
		}

		if (file instanceof TFile) {
			if (!this.previewLeaf || !this.app.workspace.getLeavesOfType("markdown").includes(this.previewLeaf)) {
				this.previewLeaf = this.app.workspace.getLeaf("split");
			}
			await this.previewLeaf.openFile(file);
		}
	}

	// ── Stage Operations ───────────────────────────────────────────────

	private async createStage(name: string) {
		const config = await this.loadConfig();
		if (!config) return;

		const slug = name.toLowerCase().replace(/[^a-z0-9_-]/g, "-");
		if (config.stages.includes(slug)) return;

		await this.app.vault.createFolder(slug);
		config.stages.push(slug);
		await this.saveConfig(config);
	}

	private async renameStage(oldName: string, newName: string) {
		const config = await this.loadConfig();
		if (!config) return;

		const slug = newName.toLowerCase().replace(/[^a-z0-9_-]/g, "-");
		if (slug === oldName || config.stages.includes(slug)) return;

		const folder = this.app.vault.getAbstractFileByPath(oldName);
		if (!(folder instanceof TFolder)) return;

		await this.app.vault.rename(folder, slug);
		config.stages = config.stages.map((s) => (s === oldName ? slug : s));
		await this.saveConfig(config);
	}

	async onClose() {}
}

// ── Text Input Modal ───────────────────────────────────────────────────

class TextInputModal extends Modal {
	private result: string;
	private readonly title: string;
	private readonly placeholder: string;
	private readonly defaultValue: string;
	private readonly onSubmit: (value: string) => void;

	constructor(
		app: import("obsidian").App,
		title: string,
		placeholder: string,
		defaultValue: string,
		onSubmit: (value: string) => void,
	) {
		super(app);
		this.title = title;
		this.placeholder = placeholder;
		this.defaultValue = defaultValue;
		this.result = defaultValue;
		this.onSubmit = onSubmit;
	}

	onOpen() {
		const { contentEl } = this;
		contentEl.createEl("h3", { text: this.title });

		new Setting(contentEl).setName("Name").addText((text) => {
			text.setPlaceholder(this.placeholder)
				.setValue(this.defaultValue)
				.onChange((value) => (this.result = value));
			// Focus and select on open
			setTimeout(() => {
				text.inputEl.focus();
				text.inputEl.select();
			}, 10);
		});

		new Setting(contentEl).addButton((btn) =>
			btn.setButtonText("Confirm").setCta().onClick(() => {
				const trimmed = this.result.trim();
				if (trimmed) {
					this.onSubmit(trimmed);
				}
				this.close();
			}),
		);
	}

	onClose() {
		this.contentEl.empty();
	}
}
