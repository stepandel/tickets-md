import {
	Plugin,
	Platform,
	ItemView,
	ViewStateResult,
	WorkspaceLeaf,
	TFile,
	TFolder,
	Menu,
	Modal,
	Setting,
	parseYaml,
} from "obsidian";

import { Terminal } from "@xterm/xterm";
import { FitAddon } from "@xterm/addon-fit";
import { html as diff2html } from "diff2html";
import { execFileSync } from "child_process";

// ── Types ──────────────────────────────────────────────────────────────

interface TicketsConfig {
	name?: string;
	prefix: string;
	stages: string[];
}

interface Ticket {
	id: string;
	title: string;
	priority?: string;
	labels?: string[];
	related?: string[];
	blocked_by?: string[];
	blocks?: string[];
	assignee?: string;
	created_at?: string;
	updated_at?: string;
	agent_status?: string;
	agent_session?: string;
	file: TFile;
	stage: string;
}

const AGENT_BADGES: Record<string, { icon: string; cls: string }> = {
	spawned:  { icon: "\u25D0", cls: "tb-agent-spawned" },
	running:  { icon: "\u25CF", cls: "tb-agent-running" },
	blocked:  { icon: "\u23F8", cls: "tb-agent-blocked" },
	done:     { icon: "\u2713", cls: "tb-agent-done" },
	failed:   { icon: "\u2717", cls: "tb-agent-failed" },
	errored:  { icon: "\u2717", cls: "tb-agent-errored" },
};

interface StageAgentConfig {
	command: string;
	args: string;
	worktree: boolean;
	baseBranch: string;
	prompt: string;
}

// ── Constants ──────────────────────────────────────────────────────────

const VIEW_TYPE = "tickets-board";
const TERMINAL_VIEW_TYPE = "tickets-terminal";
const DIFF_VIEW_TYPE = "tickets-diff";
const CONFIG_PATH = "config.yml";
const TERMINAL_SERVER_PATH = ".terminal-server";

// ── Plugin ─────────────────────────────────────────────────────────────

export default class TicketsBoardPlugin extends Plugin {
	async onload() {
		this.registerView(VIEW_TYPE, (leaf) => new BoardView(leaf));
		this.registerView(TERMINAL_VIEW_TYPE, (leaf) => new TerminalView(leaf));
		this.registerView(DIFF_VIEW_TYPE, (leaf) => new DiffView(leaf));

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
	private config: TicketsConfig | null = null;
	private stages: string[] = [];
	private tickets: Ticket[] = [];
	private agentStages: Set<string> = new Set();
	private previewLeaf: WorkspaceLeaf | null = null;

	// Touch drag state
	private dragTicketPath: string | null = null;
	private dragGhost: HTMLElement | null = null;
	private dragStartTouch: { x: number; y: number } | null = null;
	private dragActive = false;
	private longPressTriggered = false;

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
				related: fm.related,
				blocked_by: fm.blocked_by,
				blocks: fm.blocks,
				assignee: fm.assignee,
				created_at: fm.created_at,
				updated_at: fm.updated_at,
				agent_status: fm.agent_status,
				agent_session: fm.agent_session,
				file,
				stage,
			};
		} catch {
			return null;
		}
	}

	private async loadAgentStages(stages: string[]): Promise<Set<string>> {
		const result = new Set<string>();
		const adapter = this.app.vault.adapter;
		for (const stage of stages) {
			const configPath = `${stage}/.stage.yml`;
			if (await adapter.exists(configPath)) {
				try {
					const raw = await adapter.read(configPath);
					const parsed = parseYaml(raw) as { agent?: { command?: string } } | null;
					if (parsed?.agent?.command) {
						result.add(stage);
					}
				} catch { /* ignore malformed config */ }
			}
		}
		return result;
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

		this.config = config;
		this.stages = config.stages;
		this.tickets = await this.loadTickets(this.stages);
		this.agentStages = await this.loadAgentStages(this.stages);
		this.render();
	}

	private render() {
		const container = this.contentEl;
		container.empty();

		// Header
		const header = container.createDiv({ cls: "tb-header" });
		const boardName = this.config?.name || "Tickets Board";
		const titleEl = header.createEl("h2", { text: boardName, cls: "tb-board-title" });
		const openRenameModal = () => {
			new TextInputModal(
				this.app,
				"Board Name",
				"e.g. My Project",
				boardName,
				async (name) => {
					if (this.config) {
						this.config.name = name;
						await this.saveConfig(this.config);
					}
				},
			).open();
		};
		if (Platform.isMobile) {
			this.onLongPress(titleEl, () => openRenameModal());
		} else {
			titleEl.addEventListener("click", () => openRenameModal());
		}
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
		const colTitle = colHeader.createDiv({ cls: "tb-column-title" });
		colTitle.createEl("span", { text: stage, cls: "tb-stage-name" });
		if (this.agentStages.has(stage)) {
			colTitle.createEl("span", { text: "\uD83E\uDD16", cls: "tb-agent-icon", attr: { "aria-label": "Agent configured" } });
		}
		colHeader.createEl("span", {
			text: String(tickets.length),
			cls: "tb-count",
		});

		colHeader.addEventListener("contextmenu", (e) => {
			e.preventDefault();
			this.buildColumnMenu(stage).showAtMouseEvent(e);
		});
		if (Platform.isMobile) {
			this.onLongPress(colHeader, (x, y) => {
				this.buildColumnMenu(stage).showAtPosition({ x, y });
			});
		}

		// Drop zone
		const cardList = column.createDiv({ cls: "tb-card-list" });
		cardList.dataset.stage = stage;

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

		// Add ticket button
		const addBtn = cardList.createEl("button", {
			text: "+ New ticket",
			cls: "tb-add-ticket-btn",
		});
		addBtn.addEventListener("click", () => this.createTicket(stage));
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

		// Right-click context menu
		card.addEventListener("contextmenu", (e) => {
			e.preventDefault();
			this.buildCardMenu(ticket).showAtMouseEvent(e);
		});

		// Mobile: long-press for context menu
		if (Platform.isMobile) {
			this.onLongPress(card, (x, y) => {
				this.buildCardMenu(ticket).showAtPosition({ x, y });
			});
		}

		// Mobile: touch drag
		if (Platform.isMobile) {
			card.addEventListener("touchstart", (e) => {
				if (e.touches.length !== 1) {
					this.cleanupDrag();
					return;
				}
				this.dragTicketPath = ticket.file.path;
				this.dragStartTouch = { x: e.touches[0].clientX, y: e.touches[0].clientY };
				this.dragActive = false;
			});

			card.addEventListener("touchmove", (e) => {
				if (!this.dragStartTouch || !this.dragTicketPath) return;
				if (e.touches.length > 1) { this.cleanupDrag(); return; }

				const tx = e.touches[0].clientX;
				const ty = e.touches[0].clientY;
				const dx = tx - this.dragStartTouch.x;
				const dy = ty - this.dragStartTouch.y;

				if (!this.dragActive) {
					if (Math.sqrt(dx * dx + dy * dy) > 10) {
						this.dragActive = true;
						card.addClass("tb-dragging");
						this.dragGhost = this.createDragGhost(card, tx, ty);
					}
					return;
				}

				e.preventDefault();
				if (this.dragGhost) {
					this.dragGhost.style.left = `${tx - 30}px`;
					this.dragGhost.style.top = `${ty - 20}px`;
				}

				// Hit-test card lists for drag-over highlight
				this.contentEl.querySelectorAll(".tb-card-list").forEach((el) => el.removeClass("tb-drag-over"));
				const target = this.findCardListAtPoint(tx, ty);
				target?.addClass("tb-drag-over");
			});

			card.addEventListener("touchend", async (e) => {
				if (!this.dragActive) {
					this.cleanupDrag();
					return;
				}

				const touch = e.changedTouches[0];
				const target = this.findCardListAtPoint(touch.clientX, touch.clientY);
				const targetStage = target?.dataset.stage;

				if (targetStage && targetStage !== ticket.stage) {
					const file = this.app.vault.getAbstractFileByPath(this.dragTicketPath!);
					if (file instanceof TFile) {
						const newPath = `${targetStage}/${file.name}`;
						await this.app.vault.rename(file, newPath);
					}
				}

				this.cleanupDrag();
			});

			card.addEventListener("touchcancel", () => this.cleanupDrag());
		}

		// Click to open the ticket file in a split (desktop) or tab (mobile)
		card.addEventListener("click", async () => {
			if (this.longPressTriggered) {
				this.longPressTriggered = false;
				return;
			}
			if (this.dragActive) return;
			// Reuse existing preview leaf if it's still around
			if (!this.previewLeaf || !this.previewLeaf.view?.containerEl?.isConnected) {
				this.previewLeaf = this.app.workspace.getLeaf(Platform.isMobile ? "tab" : "split");
			}
			await this.previewLeaf.openFile(ticket.file);
		});

		// Card header: ID + agent badge + priority
		const cardHeader = card.createDiv({ cls: "tb-card-header" });
		const headerLeft = cardHeader.createDiv({ cls: "tb-card-header-left" });
		headerLeft.createEl("span", { text: ticket.id, cls: "tb-ticket-id" });

		if (ticket.agent_status && AGENT_BADGES[ticket.agent_status]) {
			const badge = AGENT_BADGES[ticket.agent_status];
			headerLeft.createEl("span", {
				text: badge.icon,
				cls: `tb-agent-badge ${badge.cls}`,
				attr: { "aria-label": ticket.agent_status },
			});
		}

		if (ticket.priority) {
			cardHeader.createEl("span", {
				text: ticket.priority,
				cls: `tb-priority tb-priority-${ticket.priority}`,
			});
		}

		// Title
		card.createDiv({ text: ticket.title, cls: "tb-card-title" });

		// Footer: links + labels + assignee
		const footer = card.createDiv({ cls: "tb-card-footer" });

		const linkCount =
			(ticket.related?.length ?? 0) +
			(ticket.blocked_by?.length ?? 0) +
			(ticket.blocks?.length ?? 0);

		if (linkCount > 0) {
			const linksEl = footer.createDiv({ cls: "tb-links" });
			if (linkCount > 0) {
				linksEl.createEl("span", {
					text: String(linkCount),
					cls: "tb-link-count",
					attr: { "aria-label": `${linkCount} links` },
				});
			}
			if (ticket.blocked_by && ticket.blocked_by.length > 0) {
				linksEl.createEl("span", {
					text: "blocked",
					cls: "tb-blocked-badge",
					attr: { "aria-label": `Blocked by ${ticket.blocked_by.join(", ")}` },
				});
			}
		}

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

	// ── Touch Drag Helpers ────────────────────────────────────────────

	private createDragGhost(card: HTMLElement, x: number, y: number): HTMLElement {
		const ghost = card.cloneNode(true) as HTMLElement;
		ghost.addClass("tb-drag-ghost");
		ghost.style.position = "fixed";
		ghost.style.left = `${x - 30}px`;
		ghost.style.top = `${y - 20}px`;
		ghost.style.width = `${card.offsetWidth}px`;
		document.body.appendChild(ghost);
		return ghost;
	}

	private cleanupDrag() {
		this.dragGhost?.remove();
		this.dragGhost = null;
		this.dragTicketPath = null;
		this.dragStartTouch = null;
		this.dragActive = false;
		this.contentEl.querySelectorAll(".tb-drag-over").forEach((el) => el.removeClass("tb-drag-over"));
		this.contentEl.querySelectorAll(".tb-dragging").forEach((el) => el.removeClass("tb-dragging"));
	}

	private findCardListAtPoint(x: number, y: number): HTMLElement | null {
		const lists = this.contentEl.querySelectorAll(".tb-card-list");
		for (const list of Array.from(lists)) {
			const rect = list.getBoundingClientRect();
			if (x >= rect.left && x <= rect.right && y >= rect.top && y <= rect.bottom) {
				return list as HTMLElement;
			}
		}
		return null;
	}

	// ── Long Press Helper ─────────────────────────────────────────────

	private onLongPress(el: HTMLElement, callback: (x: number, y: number) => void, delay = 500) {
		let timeout: ReturnType<typeof setTimeout> | null = null;
		let startX = 0;
		let startY = 0;

		el.addEventListener("touchstart", (e) => {
			if (e.touches.length !== 1) return;
			startX = e.touches[0].clientX;
			startY = e.touches[0].clientY;
			timeout = setTimeout(() => {
				timeout = null;
				this.longPressTriggered = true;
				navigator.vibrate?.(50);
				callback(startX, startY);
			}, delay);
		});

		el.addEventListener("touchmove", (e) => {
			if (!timeout) return;
			const dx = e.touches[0].clientX - startX;
			const dy = e.touches[0].clientY - startY;
			if (Math.sqrt(dx * dx + dy * dy) > 10) {
				clearTimeout(timeout);
				timeout = null;
			}
		});

		el.addEventListener("touchend", () => {
			if (timeout) { clearTimeout(timeout); timeout = null; }
			// Reset longPressTriggered after the click event fires (queued via setTimeout)
			if (this.longPressTriggered) {
				setTimeout(() => { this.longPressTriggered = false; }, 0);
			}
		});
		el.addEventListener("touchcancel", () => {
			if (timeout) { clearTimeout(timeout); timeout = null; }
			this.longPressTriggered = false;
		});
	}

	// ── Menu Builders ─────────────────────────────────────────────────

	private buildCardMenu(ticket: Ticket): Menu {
		const menu = new Menu();
		if (Platform.isMobile) {
			// On mobile, show log viewer for any agent that has a session (logs are static files)
			if (ticket.agent_session) {
				menu.addItem((item) =>
					item.setTitle("View agent log").setIcon("file-text").onClick(() => {
						this.openTerminal(ticket);
					}),
				);
			}
		} else if (ticket.agent_session && ticket.agent_status
			&& !["done", "failed", "errored"].includes(ticket.agent_status)) {
			menu.addItem((item) =>
				item.setTitle("Open terminal").setIcon("terminal-square").onClick(() => {
					this.openTerminal(ticket);
				}),
			);
		}
		if (ticket.agent_status) {
			menu.addItem((item) =>
				item.setTitle("View diff").setIcon("git-compare").onClick(() => {
					this.openDiff(ticket);
				}),
			);
		}
		menu.addItem((item) =>
			item.setTitle("Delete ticket").setIcon("trash").setWarning(true).onClick(() => {
				new ConfirmDeleteModal(
					this.app,
					ticket.id,
					ticket.title,
					async () => { await this.app.vault.trash(ticket.file, true); },
				).open();
			}),
		);
		return menu;
	}

	private buildColumnMenu(stage: string): Menu {
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
		return menu;
	}

	// ── Terminal ───────────────────────────────────────────────────────

	private async openTerminal(ticket: Ticket) {
		const leaf = this.app.workspace.getLeaf(Platform.isMobile ? "tab" : "split");
		await leaf.setViewState({
			type: TERMINAL_VIEW_TYPE,
			state: {
				sessionName: ticket.agent_session,
				ticketId: ticket.id,
			},
		});
		this.app.workspace.revealLeaf(leaf);
	}

	// ── Diff ──────────────────────────────────────────────────────────

	private async openDiff(ticket: Ticket) {
		const leaf = this.app.workspace.getLeaf(Platform.isMobile ? "tab" : "split");
		await leaf.setViewState({
			type: DIFF_VIEW_TYPE,
			state: { ticketId: ticket.id },
		});
		this.app.workspace.revealLeaf(leaf);
	}

	// ── Config Writing ─────────────────────────────────────────────────

	private async saveConfig(config: TicketsConfig) {
		const lines: string[] = [];
		if (config.name) {
			lines.push(`name: ${config.name}`);
		}
		lines.push(`prefix: ${config.prefix}`);
		lines.push("stages:");
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
		const adapter = this.app.vault.adapter;

		let config: StageAgentConfig = {
			command: "",
			args: "",
			worktree: false,
			baseBranch: "",
			prompt: "",
		};

		if (await adapter.exists(configPath)) {
			const raw = await adapter.read(configPath);
			const parsed = parseYaml(raw) as { agent?: Record<string, unknown> } | null;
			if (parsed?.agent) {
				const a = parsed.agent;
				config.command = String(a.command ?? "");
				config.args = Array.isArray(a.args) ? a.args.join(", ") : String(a.args ?? "");
				config.worktree = Boolean(a.worktree);
				config.baseBranch = String(a.base_branch ?? "");
				config.prompt = String(a.prompt ?? "");
			}
		}

		new StageConfigModal(this.app, stage, config, async (updated) => {
			const argsArray = updated.args
				.split(",")
				.map((s) => s.trim())
				.filter(Boolean);
			const lines: string[] = ["agent:"];
			lines.push(`    command: ${updated.command}`);
			if (argsArray.length > 0) {
				lines.push(`    args: [${argsArray.map((a) => `"${a}"`).join(", ")}]`);
			}
			if (updated.worktree) {
				lines.push("    worktree: true");
			}
			if (updated.baseBranch) {
				lines.push(`    base_branch: ${updated.baseBranch}`);
			}
			if (updated.prompt) {
				lines.push("    prompt: |");
				for (const line of updated.prompt.split("\n")) {
					lines.push(`        ${line}`);
				}
			}
			lines.push("");
			await adapter.write(configPath, lines.join("\n"));
		}).open();
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

	// ── Ticket Creation ────────────────────────────────────────────────

	private async nextTicketId(): Promise<string> {
		const config = await this.loadConfig();
		if (!config) return "TIC-001";

		const prefix = config.prefix ?? "TIC";
		const pattern = new RegExp(`^${prefix}-(\\d+)$`);
		let max = 0;

		for (const ticket of this.tickets) {
			const match = ticket.id.match(pattern);
			if (match) {
				const num = parseInt(match[1], 10);
				if (num > max) max = num;
			}
		}

		const next = max + 1;
		return `${prefix}-${String(next).padStart(3, "0")}`;
	}

	private async createTicket(stage: string) {
		const id = await this.nextTicketId();
		const now = new Date().toISOString().replace(/\.\d{3}Z$/, "Z");

		const content = [
			"---",
			`id: ${id}`,
			`title: ${id}`,
			`created_at: ${now}`,
			`updated_at: ${now}`,
			"---",
			"## Description",
			"",
			"_Describe the ticket here._",
			"",
		].join("\n");

		const file = await this.app.vault.create(`${stage}/${id}.md`, content);

		if (!this.previewLeaf || !this.previewLeaf.view?.containerEl?.isConnected) {
			this.previewLeaf = this.app.workspace.getLeaf(Platform.isMobile ? "tab" : "split");
		}
		await this.previewLeaf.openFile(file);
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

// ── Terminal View ──────────────────────────────────────────────────────

class TerminalView extends ItemView {
	private terminal: Terminal | null = null;
	private fitAddon: FitAddon | null = null;
	private ws: WebSocket | null = null;
	private sessionName = "";
	private ticketId = "";
	private resizeObserver: ResizeObserver | null = null;

	getViewType(): string {
		return TERMINAL_VIEW_TYPE;
	}

	getDisplayText(): string {
		return this.ticketId ? `Terminal: ${this.ticketId}` : "Terminal";
	}

	getIcon(): string {
		return "terminal-square";
	}

	async setState(state: Record<string, unknown>, result: ViewStateResult) {
		this.sessionName = (state.sessionName as string) ?? "";
		this.ticketId = (state.ticketId as string) ?? "";
		await super.setState(state, result);
		this.connect();
	}

	getState(): Record<string, unknown> {
		return { sessionName: this.sessionName, ticketId: this.ticketId };
	}

	async onOpen() {
		this.contentEl.addClass("tb-terminal-container");
	}

	private async connect() {
		if (Platform.isMobile) {
			await this.showAgentLog();
			return;
		}

		const serverInfo = await this.readServerFile();
		if (!serverInfo) {
			this.showError("No terminal server running. Is `tickets watch` active?");
			return;
		}

		this.terminal = new Terminal({
			cursorBlink: true,
			fontSize: 13,
			fontFamily: "var(--font-monospace), monospace",
			theme: {
				background: "#1e1e1e",
				foreground: "#d4d4d4",
				cursor: "#d4d4d4",
			},
		});
		this.fitAddon = new FitAddon();
		this.terminal.loadAddon(this.fitAddon);
		this.terminal.open(this.contentEl);
		this.fitAddon.fit();

		const url = `ws://127.0.0.1:${serverInfo.port}/terminal/${this.sessionName}`;
		this.ws = new WebSocket(url);
		this.ws.binaryType = "arraybuffer";

		this.ws.onopen = () => {
			this.sendResize();
		};

		this.ws.onmessage = (event: MessageEvent) => {
			if (event.data instanceof ArrayBuffer) {
				this.terminal?.write(new Uint8Array(event.data));
			}
		};

		this.ws.onclose = () => {
			this.terminal?.write("\r\n\x1b[90m[session ended]\x1b[0m\r\n");
		};

		this.ws.onerror = () => {
			this.showError("Connection lost to terminal server.");
		};

		this.terminal.onData((data: string) => {
			if (this.ws?.readyState === WebSocket.OPEN) {
				this.ws.send(new TextEncoder().encode(data));
			}
		});

		this.resizeObserver = new ResizeObserver(() => {
			this.fitAddon?.fit();
			this.sendResize();
		});
		this.resizeObserver.observe(this.contentEl);
	}

	private sendResize() {
		if (this.ws?.readyState === WebSocket.OPEN && this.terminal) {
			const msg = JSON.stringify({
				type: "resize",
				rows: this.terminal.rows,
				cols: this.terminal.cols,
			});
			this.ws.send(msg);
		}
	}

	private async showAgentLog() {
		const adapter = this.app.vault.adapter;
		const agentDir = `.tickets/.agents/${this.ticketId}`;

		// Check if agent directory exists
		if (!(await adapter.exists(agentDir))) {
			this.showError("No agent runs found for this ticket.");
			return;
		}

		// List files and find the latest run YAML
		const listing = await adapter.list(agentDir);
		const yamlFiles = listing.files
			.filter((f: string) => f.endsWith(".yml"))
			.sort();

		if (yamlFiles.length === 0) {
			this.showError("No agent runs found for this ticket.");
			return;
		}

		const latestYaml = yamlFiles[yamlFiles.length - 1];
		let runData: Record<string, string> = {};
		try {
			const raw = await adapter.read(latestYaml);
			runData = parseYaml(raw) ?? {};
		} catch {
			this.showError("Could not parse agent run data.");
			return;
		}

		const status = runData.status ?? "unknown";
		const runId = runData.run_id ?? latestYaml.split("/").pop()?.replace(".yml", "") ?? "";
		const logFile = runData.log_file;

		if (!logFile || !(await adapter.exists(logFile))) {
			this.showLogViewer(status, runId, null);
			return;
		}

		let logContent: string;
		try {
			logContent = await adapter.read(logFile);
		} catch {
			this.showLogViewer(status, runId, null);
			return;
		}

		// Strip ANSI escape sequences
		logContent = logContent.replace(/\x1b\[[0-9;]*[a-zA-Z]|\x1b\][^\x07]*\x07|\x1b[^[\]].?/g, "");

		this.showLogViewer(status, runId, logContent);
	}

	private showLogViewer(status: string, runId: string, logContent: string | null) {
		this.contentEl.empty();
		this.contentEl.addClass("tb-terminal-container");

		const header = this.contentEl.createDiv({ cls: "tb-log-header" });

		const badge = AGENT_BADGES[status];
		if (badge) {
			header.createEl("span", {
				text: `${badge.icon} ${status}`,
				cls: `tb-agent-badge ${badge.cls} tb-log-status`,
			});
		} else {
			header.createEl("span", { text: status, cls: "tb-log-status" });
		}

		header.createEl("span", { text: runId, cls: "tb-log-run-id" });
		header.createEl("span", {
			text: "Read-only \u2014 live terminal not available on mobile",
			cls: "tb-log-notice",
		});

		if (logContent === null) {
			this.contentEl.createEl("p", {
				text: "Log file not available.",
				cls: "tb-terminal-error",
			});
			return;
		}

		if (logContent.trim().length === 0) {
			this.contentEl.createEl("p", {
				text: "Agent session started \u2014 no output yet.",
				cls: "tb-terminal-error",
			});
			return;
		}

		const lines = logContent.split("\n");
		const MAX_INITIAL = 500;
		const viewer = this.contentEl.createDiv({ cls: "tb-log-viewer" });

		if (lines.length > MAX_INITIAL) {
			const showMoreBtn = viewer.createEl("button", {
				text: `Show earlier output (${lines.length - MAX_INITIAL} lines)`,
				cls: "tb-show-more-btn",
			});
			const pre = viewer.createEl("pre");
			pre.textContent = lines.slice(-MAX_INITIAL).join("\n");

			let shown = MAX_INITIAL;
			showMoreBtn.addEventListener("click", () => {
				shown = Math.min(shown + 500, lines.length);
				pre.textContent = lines.slice(-shown).join("\n");
				if (shown >= lines.length) {
					showMoreBtn.remove();
				} else {
					showMoreBtn.textContent = `Show earlier output (${lines.length - shown} lines)`;
				}
			});
		} else {
			const pre = viewer.createEl("pre");
			pre.textContent = logContent;
		}
	}

	private async readServerFile(): Promise<{ port: number; pid: number } | null> {
		const adapter = this.app.vault.adapter;
		if (!(await adapter.exists(TERMINAL_SERVER_PATH))) return null;
		try {
			const raw = await adapter.read(TERMINAL_SERVER_PATH);
			return JSON.parse(raw);
		} catch {
			return null;
		}
	}

	private showError(msg: string) {
		this.contentEl.empty();
		this.contentEl.addClass("tb-terminal-container");
		this.contentEl.createEl("p", { text: msg, cls: "tb-terminal-error" });
	}

	async onClose() {
		this.resizeObserver?.disconnect();
		this.ws?.close();
		this.terminal?.dispose();
	}
}

// ── Diff View ─────────────────────────────────────────────────────────

class DiffView extends ItemView {
	private ticketId = "";

	getViewType(): string {
		return DIFF_VIEW_TYPE;
	}

	getDisplayText(): string {
		return this.ticketId ? `Diff: ${this.ticketId}` : "Diff";
	}

	getIcon(): string {
		return "git-compare";
	}

	async setState(state: Record<string, unknown>, result: ViewStateResult) {
		this.ticketId = (state.ticketId as string) ?? "";
		await super.setState(state, result);
		this.renderDiff();
	}

	getState(): Record<string, unknown> {
		return { ticketId: this.ticketId };
	}

	private renderDiff() {
		this.contentEl.empty();

		if (Platform.isMobile) {
			this.showMessage("Diff view requires desktop.");
			return;
		}

		if (!this.ticketId) {
			this.showMessage("No ticket specified.");
			return;
		}

		const basePath = (this.app.vault.adapter as any).getBasePath();
		const branch = `tickets/${this.ticketId}`;
		let output: string;

		try {
			output = execFileSync("git", ["diff", `main...${branch}`], {
				cwd: basePath,
				encoding: "utf-8",
				maxBuffer: 10 * 1024 * 1024,
			});
		} catch {
			this.showMessage(`Branch "${branch}" not found or git error.`);
			return;
		}

		if (!output.trim()) {
			this.showMessage("No changes — branch is identical to main.");
			return;
		}

		const container = this.contentEl.createDiv({ cls: "tb-diff-container" });
		container.innerHTML = diff2html(output, {
			outputFormat: "line-by-line",
			drawFileList: true,
		});
	}

	private showMessage(msg: string) {
		this.contentEl.empty();
		this.contentEl.createEl("p", { text: msg, cls: "tb-diff-empty" });
	}

	async onClose() {}
}

// ── Text Input Modal ───────────────────────────────────────────────────

class StageConfigModal extends Modal {
	private config: StageAgentConfig;
	private readonly stageName: string;
	private readonly onSave: (config: StageAgentConfig) => Promise<void>;

	constructor(
		app: import("obsidian").App,
		stageName: string,
		config: StageAgentConfig,
		onSave: (config: StageAgentConfig) => Promise<void>,
	) {
		super(app);
		this.stageName = stageName;
		this.config = { ...config };
		this.onSave = onSave;
	}

	onOpen() {
		const { contentEl } = this;
		this.modalEl.addClass("tb-config-modal");

		contentEl.createEl("h3", { text: `${this.stageName} — Stage Config` });

		new Setting(contentEl)
			.setName("Command")
			.setDesc("CLI binary to invoke (e.g. claude, codex, aider)")
			.addText((text) =>
				text
					.setPlaceholder("claude")
					.setValue(this.config.command)
					.onChange((v) => (this.config.command = v)),
			);

		new Setting(contentEl)
			.setName("Args")
			.setDesc("Extra CLI flags, comma-separated")
			.addText((text) =>
				text
					.setPlaceholder("--print, --dangerously-skip-permissions")
					.setValue(this.config.args)
					.onChange((v) => (this.config.args = v)),
			);

		const promptVarsBase = "{{path}}, {{id}}, {{title}}, {{stage}}, {{body}}, {{links}}";
		const promptDescEl = contentEl.createEl("span");
		const updatePromptDesc = () => {
			const vars = this.config.worktree
				? `${promptVarsBase}, {{worktree}}`
				: promptVarsBase;
			promptDescEl.textContent = `Template with ${vars}`;
		};
		updatePromptDesc();

		new Setting(contentEl)
			.setName("Worktree")
			.setDesc("Isolate work in a git worktree per ticket")
			.addToggle((toggle) =>
				toggle
					.setValue(this.config.worktree)
					.onChange((v) => {
						this.config.worktree = v;
						updatePromptDesc();
					}),
			);

		new Setting(contentEl)
			.setName("Base branch")
			.setDesc("Branch to create worktree from (default: HEAD)")
			.addText((text) =>
				text
					.setPlaceholder("main")
					.setValue(this.config.baseBranch)
					.onChange((v) => (this.config.baseBranch = v)),
			);

		// Prompt gets a full-width textarea
		contentEl.createEl("div", {
			text: "Prompt",
			cls: "setting-item-name tb-prompt-label",
		});
		const descWrapper = contentEl.createEl("div", {
			cls: "setting-item-description tb-prompt-desc",
		});
		descWrapper.appendChild(promptDescEl);
		const promptArea = contentEl.createEl("textarea", {
			cls: "tb-config-editor",
		});
		promptArea.value = this.config.prompt;
		promptArea.spellcheck = false;
		promptArea.placeholder = 'You are the agent for {{id}}: "{{title}}".\nRead {{path}} and follow the instructions.';
		promptArea.addEventListener("input", () => {
			this.config.prompt = promptArea.value;
		});

		new Setting(contentEl).addButton((btn) =>
			btn.setButtonText("Save").setCta().onClick(async () => {
				await this.onSave(this.config);
				this.close();
			}),
		).addButton((btn) =>
			btn.setButtonText("Cancel").onClick(() => this.close()),
		);
	}

	onClose() {
		this.contentEl.empty();
	}
}

class ConfirmDeleteModal extends Modal {
	private readonly ticketId: string;
	private readonly ticketTitle: string;
	private readonly onConfirm: () => Promise<void>;

	constructor(
		app: import("obsidian").App,
		ticketId: string,
		ticketTitle: string,
		onConfirm: () => Promise<void>,
	) {
		super(app);
		this.ticketId = ticketId;
		this.ticketTitle = ticketTitle;
		this.onConfirm = onConfirm;
	}

	onOpen() {
		const { contentEl } = this;
		this.modalEl.addClass("tb-confirm-delete-modal");

		contentEl.createEl("h3", { text: "Delete ticket" });
		contentEl.createEl("p", {
			text: `Are you sure you want to delete ${this.ticketId} (${this.ticketTitle})?`,
		});

		new Setting(contentEl).addButton((btn) =>
			btn.setButtonText("Delete").setWarning().onClick(async () => {
				await this.onConfirm();
				this.close();
			}),
		).addButton((btn) =>
			btn.setButtonText("Cancel").onClick(() => this.close()),
		);
	}

	onClose() {
		this.contentEl.empty();
	}
}

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
