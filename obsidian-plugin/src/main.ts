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
	Notice,
	Setting,
	FuzzySuggestModal,
	parseYaml,
	stringifyYaml,
} from "obsidian";

import { Terminal } from "@xterm/xterm";
import { FitAddon } from "@xterm/addon-fit";
import { html as diff2html } from "diff2html";

// ── Types ──────────────────────────────────────────────────────────────

interface TicketsConfig {
	name?: string;
	prefix: string;
	project_prefix?: string;
	stages: string[];
	default_agent?: { command: string; args?: string[] };
	cron_agents?: CronAgentConfig[];
}

interface Ticket {
	id: string;
	title: string;
	priority?: string;
	project?: string;
	related?: string[];
	blocked_by?: string[];
	blocks?: string[];
	parent?: string;
	children?: string[];
	created_at?: string;
	updated_at?: string;
	agent_status?: string;
	agent_session?: string;
	file: TFile;
	stage: string;
}

interface Project {
	id: string;
	title: string;
	status?: string;
	created_at?: string;
	updated_at?: string;
	body: string;
	file: TFile;
}

interface CronAgentConfig {
	name: string;
	schedule: string;
	command: string;
	args?: string[];
	prompt: string;
	worktree?: boolean;
	base_branch?: string;
	enabled?: boolean;
}

interface AgentRun {
	run_id: string;
	status?: string;
	session?: string;
	spawned_at?: string;
	updated_at?: string;
	log_file?: string;
}

interface CronAgentRow {
	config: CronAgentConfig;
	lastRun: AgentRun | null;
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

interface EditableCronAgentConfig {
	name: string;
	schedule: string;
	command: string;
	args: string;
	prompt: string;
	enabled: boolean;
}

// ── Constants ──────────────────────────────────────────────────────────

const VIEW_TYPE = "tickets-board";
const TERMINAL_VIEW_TYPE = "tickets-terminal";
const DIFF_VIEW_TYPE = "tickets-diff";
const AGENTS_VIEW_TYPE = "tickets-agents";
const PROJECTS_VIEW_TYPE = "tickets-projects";
const CONFIG_PATH = "config.yml";
const TERMINAL_SERVER_PATH = ".terminal-server";
const PROJECTS_DIR = "projects";

const ACTIVE_AGENT_STATUSES = ["spawned", "running", "blocked"];
const AGENTS_VIEW_STATUSES = [...ACTIVE_AGENT_STATUSES, "failed", "errored"];

// ── Shared helpers ─────────────────────────────────────────────────────

async function loadConfig(app: import("obsidian").App): Promise<TicketsConfig | null> {
	const file = app.vault.getAbstractFileByPath(CONFIG_PATH);
	if (!(file instanceof TFile)) return null;
	const raw = await app.vault.read(file);
	return parseYaml(raw) as TicketsConfig;
}

async function writeConfig(app: import("obsidian").App, config: TicketsConfig): Promise<void> {
	const file = app.vault.getAbstractFileByPath(CONFIG_PATH);
	if (!(file instanceof TFile)) return;
	await app.vault.modify(file, stringifyYaml(config));
}

function nowTimestamp(): string {
	return new Date().toISOString().replace(/\.\d{3}Z$/, "Z");
}

async function readFrontmatterFile(
	app: import("obsidian").App,
	file: TFile,
): Promise<{ frontmatter: Record<string, any>; body: string } | null> {
	const content = await app.vault.read(file);
	const match = content.match(/^---\n([\s\S]*?)\n---/);
	if (!match) return null;
	try {
		return {
			frontmatter: (parseYaml(match[1]) ?? {}) as Record<string, any>,
			body: content.slice(match[0].length),
		};
	} catch {
		return null;
	}
}

function serializeFrontmatterContent(frontmatter: Record<string, any>, body: string): string {
	return "---\n" + stringifyYaml(frontmatter) + "---" + body;
}

async function updateFrontmatterFile(
	app: import("obsidian").App,
	file: TFile,
	mutate: (fm: Record<string, any>) => void,
): Promise<void> {
	const parsed = await readFrontmatterFile(app, file);
	if (!parsed) return;
	mutate(parsed.frontmatter);
	parsed.frontmatter.updated_at = nowTimestamp();
	for (const key of Object.keys(parsed.frontmatter)) {
		const value = parsed.frontmatter[key];
		if (value === undefined || value === null || value === "" || (Array.isArray(value) && value.length === 0)) {
			delete parsed.frontmatter[key];
		}
	}
	await app.vault.modify(file, serializeFrontmatterContent(parsed.frontmatter, parsed.body));
}

async function parseTicket(app: import("obsidian").App, file: TFile, stage: string): Promise<Ticket | null> {
	const content = await app.vault.read(file);
	const match = content.match(/^---\n([\s\S]*?)\n---/);
	if (!match) return null;

	try {
		const fm = parseYaml(match[1]);
		return {
			id: fm.id ?? file.basename,
			title: fm.title ?? file.basename,
			priority: fm.priority,
			project: fm.project,
			related: fm.related,
			blocked_by: fm.blocked_by,
			blocks: fm.blocks,
			parent: fm.parent,
			children: fm.children,
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

async function parseProject(app: import("obsidian").App, file: TFile): Promise<Project | null> {
	const parsed = await readFrontmatterFile(app, file);
	if (!parsed) return null;

	try {
		const fm = parsed.frontmatter;
		return {
			id: fm.id ?? file.basename,
			title: fm.title ?? file.basename,
			status: fm.status,
			created_at: fm.created_at,
			updated_at: fm.updated_at,
			body: parsed.body.trimStart(),
			file,
		};
	} catch {
		return null;
	}
}

async function loadProjects(app: import("obsidian").App, config: TicketsConfig): Promise<Project[]> {
	const projects: Project[] = [];
	const folder = app.vault.getAbstractFileByPath(PROJECTS_DIR);
	if (!(folder instanceof TFolder)) return projects;

	const prefix = config.project_prefix ?? "PRJ";
	const pattern = new RegExp(`^${escapeRegExp(prefix)}-\\d+\\.md$`);

	for (const child of folder.children) {
		if (!(child instanceof TFile) || child.extension !== "md") continue;
		if (!pattern.test(child.name)) continue;

		const project = await parseProject(app, child);
		if (project) projects.push(project);
	}

	return projects.sort((a, b) => a.id.localeCompare(b.id, undefined, { numeric: true }));
}

function escapeRegExp(text: string): string {
	return text.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
}

async function nextProjectId(app: import("obsidian").App, config: TicketsConfig): Promise<string> {
	const prefix = config.project_prefix ?? "PRJ";
	const pattern = new RegExp(`^${escapeRegExp(prefix)}-(\\d+)\\.md$`);
	const folder = app.vault.getAbstractFileByPath(PROJECTS_DIR);
	if (!(folder instanceof TFolder)) {
		return `${prefix}-001`;
	}

	let max = 0;
	for (const child of folder.children) {
		if (!(child instanceof TFile)) continue;
		const match = child.name.match(pattern);
		if (!match) continue;
		const n = Number.parseInt(match[1], 10);
		if (!Number.isNaN(n) && n > max) {
			max = n;
		}
	}

	return `${prefix}-${String(max + 1).padStart(3, "0")}`;
}

function openPreviewLeaf(app: import("obsidian").App, previewLeaf: WorkspaceLeaf | null): WorkspaceLeaf {
	if (previewLeaf?.view?.containerEl?.isConnected) {
		return previewLeaf;
	}
	return app.workspace.getLeaf(Platform.isMobile ? "tab" : "split");
}

async function loadTickets(app: import("obsidian").App, stages: string[]): Promise<Ticket[]> {
	const tickets: Ticket[] = [];
	for (const stage of stages) {
		const folder = app.vault.getAbstractFileByPath(stage);
		if (!(folder instanceof TFolder)) continue;

		for (const child of folder.children) {
			if (!(child instanceof TFile) || child.extension !== "md") continue;
			if (child.name.startsWith(".")) continue;

			const ticket = await parseTicket(app, child, stage);
			if (ticket) tickets.push(ticket);
		}
	}
	return tickets;
}

function cronAgentEnabled(config: CronAgentConfig): boolean {
	return config.enabled !== false;
}

function cronAgentEntriesEqual(a: CronAgentConfig, b: CronAgentConfig): boolean {
	return stringifyYaml(a) === stringifyYaml(b);
}

function parseCronAgentConfig(config: CronAgentConfig): EditableCronAgentConfig {
	return {
		name: config.name,
		schedule: config.schedule ?? "",
		command: config.command ?? "",
		args: Array.isArray(config.args) ? config.args.join(", ") : "",
		prompt: config.prompt ?? "",
		enabled: cronAgentEnabled(config),
	};
}

function formatElapsedSince(timestamp?: string): string {
	if (!timestamp) return "\u2014";
	const then = new Date(timestamp);
	if (Number.isNaN(then.getTime())) return "\u2014";
	const diffMs = Date.now() - then.getTime();
	if (diffMs < 0) return "\u2014";
	const totalSeconds = Math.floor(diffMs / 1000);
	if (totalSeconds < 60) return `${totalSeconds}s`;
	if (totalSeconds < 3600) return `${Math.floor(totalSeconds / 60)}m`;
	if (totalSeconds < 86400) return `${Math.floor(totalSeconds / 3600)}h`;
	return `${Math.floor(totalSeconds / 86400)}d`;
}

function validateCronAgent(config: EditableCronAgentConfig): string | null {
	if (!config.name.trim()) return "Cron name is required";
	if (!config.schedule.trim()) return "Cron schedule is required";
	if (!config.command.trim()) return "Cron command is required";
	if (!config.prompt.trim()) return "Cron prompt is required";
	return null;
}

function isCronRunPath(path: string): boolean {
	return path.startsWith(".agents/.cron/") && path.endsWith(".yml");
}

function cronLogPath(name: string, runID: string): string {
	return `.agents/.cron/${name}/runs/${runID}.log`;
}

async function loadLatestCronRun(
	app: import("obsidian").App,
	name: string,
): Promise<AgentRun | null> {
	const adapter = app.vault.adapter;
	const dir = `.agents/.cron/${name}`;
	if (!(await adapter.exists(dir))) return null;

	try {
		const listed = await adapter.list(dir);
		const latest = listed.files
			.filter((path) => path.endsWith(".yml"))
			.map((path) => {
				const base = path.split("/").pop() ?? path;
				const seq = parseInt(base.match(/^(\d+)-/)?.[1] ?? "", 10);
				return { path, seq: Number.isFinite(seq) ? seq : -1 };
			})
			.sort((a, b) => b.seq - a.seq || b.path.localeCompare(a.path))[0];
		if (!latest) return null;
		const raw = await adapter.read(latest.path);
		return (parseYaml(raw) ?? null) as AgentRun | null;
	} catch {
		return null;
	}
}

function hasActiveAgent(ticket: Ticket): boolean {
	return !!ticket.agent_status && ACTIVE_AGENT_STATUSES.includes(ticket.agent_status);
}

function canRerunStageAgent(ticket: Ticket): boolean {
	return !ticket.agent_status || !hasActiveAgent(ticket);
}

async function readServerFile(app: import("obsidian").App): Promise<{ port: number; pid: number } | null> {
	const adapter = app.vault.adapter;
	if (!(await adapter.exists(TERMINAL_SERVER_PATH))) return null;
	try {
		const raw = await adapter.read(TERMINAL_SERVER_PATH);
		return JSON.parse(raw);
	} catch {
		return null;
	}
}

async function openTerminal(app: import("obsidian").App, ticket: Ticket) {
	const leaf = app.workspace.getLeaf(Platform.isMobile ? "tab" : "split");
	await leaf.setViewState({
		type: TERMINAL_VIEW_TYPE,
		state: {
			sessionName: ticket.agent_session,
			ticketId: ticket.id,
		},
	});
	app.workspace.revealLeaf(leaf);
}

type TerminalSpawnBody = Record<string, string | number>;

async function openDiff(app: import("obsidian").App, ticket: Ticket) {
	const leaf = app.workspace.getLeaf(Platform.isMobile ? "tab" : "split");
	await leaf.setViewState({
		type: DIFF_VIEW_TYPE,
		state: { ticketId: ticket.id },
	});
	app.workspace.revealLeaf(leaf);
}

// openSpawningTerminal opens a TerminalView leaf in "pending spawn"
// mode: the view measures its container, posts the supplied spawn body
// plus {rows, cols} to the given terminal-server endpoint, and only then connects the
// WebSocket. Threading the geometry through avoids the first-second
// 24x120 wrap that happens when the PTY starts before the client's
// first resize message.
async function openSpawningTerminal(
	app: import("obsidian").App,
	sessionLabel: string,
	path: string,
	label: string,
	spawnBody: TerminalSpawnBody,
) {
	const serverInfo = await readServerFile(app);
	if (!serverInfo) {
		new Notice("terminal server not running — start `tickets watch`");
		return;
	}
	const leaf = app.workspace.getLeaf("split");
	await leaf.setViewState({
		type: TERMINAL_VIEW_TYPE,
		state: {
			ticketId: sessionLabel,
			spawnPath: path,
			spawnLabel: label,
			spawnBody,
		},
	});
	app.workspace.revealLeaf(leaf);
}

async function rerunStageAgent(app: import("obsidian").App, ticket: Ticket) {
	await openSpawningTerminal(app, ticket.id, "/rerun-stage-agent", "re-run stage agent", { ticket_id: ticket.id });
}

async function runCronAgent(app: import("obsidian").App, name: string) {
	await openSpawningTerminal(app, name, "/run-cron-agent", `run cron ${name}`, { name });
}

// ── Plugin ─────────────────────────────────────────────────────────────

export default class TicketsBoardPlugin extends Plugin {
	async onload() {
		this.registerView(VIEW_TYPE, (leaf) => new BoardView(leaf));
		this.registerView(TERMINAL_VIEW_TYPE, (leaf) => new TerminalView(leaf));
		this.registerView(DIFF_VIEW_TYPE, (leaf) => new DiffView(leaf));
		this.registerView(AGENTS_VIEW_TYPE, (leaf) => new AgentsView(leaf));
		this.registerView(PROJECTS_VIEW_TYPE, (leaf) => new ProjectsView(leaf));

		this.addRibbonIcon("kanban", "Tickets Board", () => this.activateView());
		this.addRibbonIcon("bot", "Tickets Agents", () => this.activateAgentsView());
		this.addRibbonIcon("folder-open", "Tickets Projects", () => this.activateProjectsView());

		this.addCommand({
			id: "open-tickets-board",
			name: "Open Tickets Board",
			callback: () => this.activateView(),
		});

		this.addCommand({
			id: "open-tickets-agents",
			name: "Open Tickets Agents",
			callback: () => this.activateAgentsView(),
		});

		this.addCommand({
			id: "open-tickets-projects",
			name: "Open Tickets Projects",
			callback: () => this.activateProjectsView(),
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

	async activateAgentsView() {
		const { workspace } = this.app;

		let leaf = workspace.getLeavesOfType(AGENTS_VIEW_TYPE)[0];
		if (!leaf) {
			leaf = workspace.getLeaf("tab");
			await leaf.setViewState({ type: AGENTS_VIEW_TYPE, active: true });
		}
		workspace.revealLeaf(leaf);
	}

	async activateProjectsView() {
		const { workspace } = this.app;

		let leaf = workspace.getLeavesOfType(PROJECTS_VIEW_TYPE)[0];
		if (!leaf) {
			leaf = workspace.getLeaf("tab");
			await leaf.setViewState({ type: PROJECTS_VIEW_TYPE, active: true });
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
	private projects: Project[] = [];
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

	private async updateTicketFrontmatter(
		file: TFile,
		mutate: (fm: Record<string, any>) => void,
	): Promise<void> {
		await updateFrontmatterFile(this.app, file, mutate);
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

	private wouldCreateParentCycle(childID: string, parentID: string, ticketsById: Map<string, Ticket>): boolean {
		let current = ticketsById.get(parentID);
		for (let i = 0; current && i < ticketsById.size; i++) {
			if (current.id === childID) return true;
			if (!current.parent) return false;
			current = ticketsById.get(current.parent);
		}
		return current !== undefined;
	}

	// ── Rendering ──────────────────────────────────────────────────────

	private async refresh() {
		const config = await loadConfig(this.app);
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
		this.tickets = await loadTickets(this.app, this.stages);
		this.projects = await loadProjects(this.app, config);
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

		// Footer: links
		const footer = card.createDiv({ cls: "tb-card-footer" });

		const linkCount =
			(ticket.related?.length ?? 0) +
			(ticket.blocked_by?.length ?? 0) +
			(ticket.blocks?.length ?? 0) +
			(ticket.children?.length ?? 0) +
			(ticket.parent ? 1 : 0);

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
			if (ticket.blocks && ticket.blocks.length > 0) {
				linksEl.createEl("span", {
					text: "blocks",
					cls: "tb-blocks-badge",
					attr: { "aria-label": `Blocks ${ticket.blocks.join(", ")}` },
				});
			}
			if (ticket.parent) {
				linksEl.createEl("span", {
					text: `↑ ${ticket.parent}`,
					cls: "tb-blocks-badge",
					attr: { "aria-label": `Child of ${ticket.parent}` },
				});
			}
			if (ticket.children && ticket.children.length > 0) {
				linksEl.createEl("span", {
					text: `↳ ${ticket.children.length}`,
					cls: "tb-link-count",
					attr: { "aria-label": `${ticket.children.length} sub-tickets` },
				});
			}
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

		// Group 1 — Quick actions
		menu.addItem((item) =>
			item.setTitle("Copy ticket ID").setIcon("copy").onClick(async () => {
				await navigator.clipboard.writeText(ticket.id);
				new Notice(`Copied ${ticket.id}`);
			}),
		);

		// Group 2 — Metadata
		menu.addSeparator();
		menu.addItem((item) =>
			item.setTitle("Set priority").setIcon("alert-triangle").onClick(() => {
				const options = ["critical", "high", "medium", "low", "none"];
				new FuzzyPickerModal<string>(
					this.app,
					options,
					(v) => v,
					async (choice) => {
						await this.updateTicketFrontmatter(ticket.file, (fm) => {
							fm.priority = choice === "none" ? undefined : choice;
						});
					},
				).open();
			}),
		);
		const alternateProjects = this.projects.filter((project) => project.id !== ticket.project);
		if (!ticket.project && this.projects.length > 0) {
			menu.addItem((item) =>
				item.setTitle("Assign to project").setIcon("folder-plus").onClick(() => {
					new FuzzyPickerModal<Project>(
						this.app,
						this.projects,
						(project) => `${project.id} — ${project.title}`,
						async (project) => {
							await this.updateTicketFrontmatter(ticket.file, (fm) => {
								fm.project = project.id;
							});
						},
					).open();
				}),
			);
		}
		if (ticket.project && alternateProjects.length > 0) {
			menu.addItem((item) =>
				item.setTitle("Change project").setIcon("folder-sync").onClick(() => {
					new FuzzyPickerModal<Project>(
						this.app,
						alternateProjects,
						(project) => `${project.id} — ${project.title}`,
						async (project) => {
							await this.updateTicketFrontmatter(ticket.file, (fm) => {
								fm.project = project.id;
							});
						},
					).open();
				}),
			);
		}
		if (ticket.project) {
			menu.addItem((item) =>
				item.setTitle("Unassign project").setIcon("folder-x").onClick(async () => {
					await this.updateTicketFrontmatter(ticket.file, (fm) => {
						delete fm.project;
					});
				}),
			);
		}

		// Group 3 — Move
		const otherStages = this.stages.filter((s) => s !== ticket.stage);
		if (otherStages.length > 0) {
			menu.addSeparator();
			for (const target of otherStages) {
				menu.addItem((item) =>
					item.setTitle(`Move to ${target}`).setIcon("arrow-right").onClick(async () => {
						await this.app.vault.rename(ticket.file, `${target}/${ticket.file.name}`);
					}),
				);
			}
		}

		// Group 4 — Links
		const alreadyRelated = new Set(ticket.related ?? []);
		const alreadyBlockedBy = new Set(ticket.blocked_by ?? []);
		const childIDs = new Set(ticket.children ?? []);
		const linkCandidates = this.tickets.filter(
			(t) => t.id !== ticket.id && !alreadyRelated.has(t.id),
		);
		const blockCandidates = this.tickets.filter(
			(t) => t.id !== ticket.id && !alreadyBlockedBy.has(t.id),
		);
		const ticketsById = new Map(this.tickets.map((t) => [t.id, t]));
		const parentCandidates = this.tickets.filter(
			(t) =>
				t.id !== ticket.id &&
				t.id !== ticket.parent &&
				!childIDs.has(t.id) &&
				!this.wouldCreateParentCycle(ticket.id, t.id, ticketsById),
		);

		type LinkItem = { id: string; type: "related" | "blocked_by" | "blocks" | "parent" | "child"; label: string };
		const unlinkItems: LinkItem[] = [];
		for (const rid of ticket.related ?? []) {
			const t = ticketsById.get(rid);
			if (t) unlinkItems.push({ id: rid, type: "related", label: `${rid} — ${t.title} (related)` });
		}
		for (const rid of ticket.blocked_by ?? []) {
			const t = ticketsById.get(rid);
			if (t) unlinkItems.push({ id: rid, type: "blocked_by", label: `${rid} — ${t.title} (blocked by)` });
		}
		for (const rid of ticket.blocks ?? []) {
			const t = ticketsById.get(rid);
			if (t) unlinkItems.push({ id: rid, type: "blocks", label: `${rid} — ${t.title} (blocks)` });
		}
		if (ticket.parent) {
			const t = ticketsById.get(ticket.parent);
			if (t) unlinkItems.push({ id: ticket.parent, type: "parent", label: `${ticket.parent} — ${t.title} (parent)` });
		}
		for (const rid of ticket.children ?? []) {
			const t = ticketsById.get(rid);
			if (t) unlinkItems.push({ id: rid, type: "child", label: `${rid} — ${t.title} (child)` });
		}

		const hasLinkSection = linkCandidates.length > 0 || blockCandidates.length > 0 || parentCandidates.length > 0 || ticket.parent !== undefined || unlinkItems.length > 0;
		if (hasLinkSection) {
			menu.addSeparator();
			if (linkCandidates.length > 0) {
				menu.addItem((item) =>
					item.setTitle("Link related").setIcon("link").onClick(() => {
						new FuzzyPickerModal<Ticket>(
							this.app,
							linkCandidates,
							(t) => `${t.id} — ${t.title}`,
							async (target) => {
								await this.updateTicketFrontmatter(ticket.file, (fm) => {
									fm.related = [...(fm.related ?? []), target.id];
								});
								try {
									await this.updateTicketFrontmatter(target.file, (fm) => {
										fm.related = [...(fm.related ?? []), ticket.id];
									});
								} catch { /* target file may be gone */ }
							},
						).open();
					}),
				);
			}
			if (blockCandidates.length > 0) {
				menu.addItem((item) =>
					item.setTitle("Mark as blocked by").setIcon("shield-ban").onClick(() => {
						new FuzzyPickerModal<Ticket>(
							this.app,
							blockCandidates,
							(t) => `${t.id} — ${t.title}`,
							async (target) => {
								await this.updateTicketFrontmatter(ticket.file, (fm) => {
									fm.blocked_by = [...(fm.blocked_by ?? []), target.id];
								});
								try {
									await this.updateTicketFrontmatter(target.file, (fm) => {
										fm.blocks = [...(fm.blocks ?? []), ticket.id];
									});
								} catch { /* target file may be gone */ }
							},
						).open();
					}),
				);
			}
			if (!ticket.parent && parentCandidates.length > 0) {
				menu.addItem((item) =>
					item.setTitle("Set parent").setIcon("git-merge").onClick(() => {
						new FuzzyPickerModal<Ticket>(
							this.app,
							parentCandidates,
							(t) => `${t.id} — ${t.title}`,
							async (target) => {
								await this.updateTicketFrontmatter(ticket.file, (fm) => {
									fm.parent = target.id;
								});
								try {
									await this.updateTicketFrontmatter(target.file, (fm) => {
										fm.children = [...(fm.children ?? []), ticket.id];
									});
								} catch { /* target file may be gone */ }
							},
						).open();
					}),
				);
			}
			if (ticket.parent) {
				menu.addItem((item) =>
					item.setTitle("Unset parent").setIcon("git-pull-request-draft").onClick(async () => {
						const parentID = ticket.parent;
						const target = parentID ? ticketsById.get(parentID) : undefined;
						await this.updateTicketFrontmatter(ticket.file, (fm) => {
							delete fm.parent;
						});
						if (!target) return;
						try {
							await this.updateTicketFrontmatter(target.file, (fm) => {
								if (Array.isArray(fm.children)) {
									fm.children = fm.children.filter((x: string) => x !== ticket.id);
								}
							});
						} catch { /* target file may be gone */ }
					}),
				);
			}
			menu.addItem((item) =>
				item.setTitle("Add sub-ticket").setIcon("list-tree").onClick(() => {
					new TextInputModal(
						this.app,
						"New sub-ticket",
						"Sub-ticket title",
						"",
						async (title) => {
							const defaultStage = this.config?.stages[0] ?? ticket.stage;
							await this.createTicket(defaultStage, title, ticket);
						},
					).open();
				}),
			);
			if (unlinkItems.length > 0) {
				menu.addItem((item) =>
					item.setTitle("Unlink").setIcon("unlink").onClick(() => {
						new FuzzyPickerModal<LinkItem>(
							this.app,
							unlinkItems,
							(it) => it.label,
							async (choice) => {
								const target = ticketsById.get(choice.id);
								await this.updateTicketFrontmatter(ticket.file, (fm) => {
									const remove = (key: string) => {
										if (Array.isArray(fm[key])) {
											fm[key] = fm[key].filter((x: string) => x !== choice.id);
										}
									};
									if (choice.type === "parent") {
										delete fm.parent;
									} else if (choice.type === "child") {
										remove("children");
									} else {
										remove(choice.type);
									}
								});
								if (!target) return;
								try {
									await this.updateTicketFrontmatter(target.file, (fm) => {
										const reverseKey =
											choice.type === "related" ? "related" :
											choice.type === "blocked_by" ? "blocks" :
											choice.type === "blocks" ? "blocked_by" :
											choice.type === "parent" ? "children" :
											"parent";
										if (reverseKey === "parent") {
											if (fm.parent === ticket.id) {
												delete fm.parent;
											}
										} else if (Array.isArray(fm[reverseKey])) {
											fm[reverseKey] = fm[reverseKey].filter((x: string) => x !== ticket.id);
										}
									});
								} catch { /* target file may be gone */ }
							},
						).open();
					}),
				);
			}
		}

		// Group 5 — Agent / danger
		menu.addSeparator();
		if (!Platform.isMobile && ticket.agent_session && ticket.agent_status
			&& !["done", "failed", "errored"].includes(ticket.agent_status)) {
			menu.addItem((item) =>
				item.setTitle("Open terminal").setIcon("terminal-square").onClick(() => {
					openTerminal(this.app, ticket);
				}),
			);
		}
		// Manual agent triggers — only shown on desktop, and only when the
		// ticket has no active agent run.
		if (!Platform.isMobile) {
			if (canRerunStageAgent(ticket)) {
				menu.addItem((item) =>
					item.setTitle("Re-run stage agent").setIcon("refresh-cw").onClick(() => {
						rerunStageAgent(this.app, ticket);
					}),
				);
				if (this.config?.default_agent?.command) {
					menu.addItem((item) =>
						item.setTitle("Adhoc agent run").setIcon("bot").onClick(() => {
							openSpawningTerminal(this.app, ticket.id, "/spawn", "spawn agent run", { ticket_id: ticket.id });
						}),
					);
				}
			}
		}
		if (ticket.agent_status) {
			menu.addItem((item) =>
				item.setTitle("View diff").setIcon("git-compare").onClick(() => {
					openDiff(this.app, ticket);
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


	// ── Config Writing ─────────────────────────────────────────────────

	private async saveConfig(config: TicketsConfig) {
		await writeConfig(this.app, config);
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
		const config = await loadConfig(this.app);
		if (!config) return;

		const slug = name.toLowerCase().replace(/[^a-z0-9_-]/g, "-");
		if (config.stages.includes(slug)) return;

		await this.app.vault.createFolder(slug);
		config.stages.push(slug);
		await this.saveConfig(config);
	}

	// ── Ticket Creation ────────────────────────────────────────────────

	private async nextTicketId(): Promise<string> {
		const config = await loadConfig(this.app);
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

	private async createTicket(stage: string, title?: string, parentTicket?: Ticket) {
		const id = await this.nextTicketId();
		const now = new Date().toISOString().replace(/\.\d{3}Z$/, "Z");
		const ticketTitle = title?.trim() || id;

		const content = [
			"---",
			`id: ${id}`,
			`title: ${ticketTitle}`,
			...(parentTicket ? [`parent: ${parentTicket.id}`] : []),
			`created_at: ${now}`,
			`updated_at: ${now}`,
			"---",
			"## Description",
			"",
			"_Describe the ticket here._",
			"",
		].join("\n");

		const file = await this.app.vault.create(`${stage}/${id}.md`, content);
		if (parentTicket) {
			try {
				await this.updateTicketFrontmatter(parentTicket.file, (fm) => {
					fm.children = [...(fm.children ?? []), id];
				});
			} catch { /* parent file may be gone */ }
		}

		if (!this.previewLeaf || !this.previewLeaf.view?.containerEl?.isConnected) {
			this.previewLeaf = this.app.workspace.getLeaf(Platform.isMobile ? "tab" : "split");
		}
		await this.previewLeaf.openFile(file);
	}

	private async renameStage(oldName: string, newName: string) {
		const config = await loadConfig(this.app);
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
	private spawnPath = "";
	private spawnLabel = "";
	private spawnBody: TerminalSpawnBody = {};
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
		this.spawnPath = (state.spawnPath as string) ?? "";
		this.spawnLabel = (state.spawnLabel as string) ?? "";
		this.spawnBody = ((state.spawnBody as TerminalSpawnBody | undefined) ?? {});
		await super.setState(state, result);
		this.start();
	}

	getState(): Record<string, unknown> {
		// Persist only the attached session — re-running a spawn after a
		// reload would create a duplicate run, so we drop spawnPath here.
		return { sessionName: this.sessionName, ticketId: this.ticketId };
	}

	async onOpen() {
		this.contentEl.addClass("tb-terminal-container");
	}

	private async start() {
		const serverInfo = await readServerFile(this.app);
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

		// In spawn mode the PTY hasn't been created yet — POST the
		// measured geometry first so the agent's first output already
		// fits the visible viewport.
		if (this.spawnPath) {
			const session = await this.postSpawn(serverInfo.port);
			if (!session) return;
			this.sessionName = session;
			this.spawnPath = "";
		}

		this.connectWebSocket(serverInfo.port);

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

	private async postSpawn(port: number): Promise<string | null> {
		const label = this.spawnLabel || "spawn agent";
		try {
			const resp = await fetch(`http://127.0.0.1:${port}${this.spawnPath}`, {
				method: "POST",
				headers: { "Content-Type": "application/json" },
				body: JSON.stringify({
					...this.spawnBody,
					rows: this.terminal?.rows ?? 0,
					cols: this.terminal?.cols ?? 0,
				}),
			});
			if (!resp.ok) {
				const text = (await resp.text()).trim();
				console.error(`${label} failed:`, text);
				new Notice(`${label}: ${text || resp.statusText}`);
				this.showError(`${label}: ${text || resp.statusText}`);
				return null;
			}
			const { session } = (await resp.json()) as { session: string };
			return session;
		} catch (e) {
			console.error(`${label}:`, e);
			const msg = e instanceof Error ? e.message : String(e);
			new Notice(`${label}: ${msg}`);
			this.showError(`${label}: ${msg}`);
			return null;
		}
	}

	private connectWebSocket(port: number) {
		// Pass initial geometry on the upgrade so the PTY resizes before
		// the replay buffer is rendered (matters for sessions that were
		// started by the file watcher with no client geometry available).
		const rows = this.terminal?.rows ?? 0;
		const cols = this.terminal?.cols ?? 0;
		const url =
			`ws://127.0.0.1:${port}/terminal/${this.sessionName}` +
			`?rows=${rows}&cols=${cols}`;
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
		const repoRoot = basePath.replace(/\/\.tickets$/, "");
		const worktreePath = `${repoRoot}/.worktrees/${this.ticketId}`;
		let output: string;

		try {
			const { execFileSync } = require("child_process");
			const fs = require("fs");
			const execOpts = { cwd: basePath, encoding: "utf-8" as const, maxBuffer: 10 * 1024 * 1024 };
			let defaultBranch = "main";
			try {
				defaultBranch = execFileSync(
					"git", ["rev-parse", "--abbrev-ref", "origin/HEAD"],
					execOpts,
				).trim();
			} catch {
				// origin/HEAD not set — check if "main" exists, otherwise try "master"
				try {
					execFileSync("git", ["rev-parse", "--verify", "main"], execOpts);
				} catch {
					defaultBranch = "master";
				}
			}

			if (fs.existsSync(worktreePath)) {
				try {
					const base = execFileSync("git", ["merge-base", "HEAD", defaultBranch], {
						cwd: worktreePath,
						encoding: "utf-8",
						maxBuffer: 10 * 1024 * 1024,
					}).trim();
					if (!base) throw new Error("no merge-base");
					output = execFileSync("git", ["diff", `${base}...HEAD`], {
						cwd: worktreePath,
						encoding: "utf-8",
						maxBuffer: 10 * 1024 * 1024,
					});
				} catch (err) {
					console.error(err);
					output = execFileSync("git", ["diff", `${defaultBranch}...HEAD`], {
						cwd: worktreePath,
						encoding: "utf-8",
						maxBuffer: 10 * 1024 * 1024,
					});
				}
			} else {
				const branch = `tickets/${this.ticketId}`;
				output = execFileSync("git", ["diff", `${defaultBranch}...${branch}`], {
					cwd: basePath,
					encoding: "utf-8",
					maxBuffer: 10 * 1024 * 1024,
				});
			}
		} catch {
			this.showMessage(`Could not compute diff for ${this.ticketId}.`);
			return;
		}

		if (!output.trim()) {
			this.showMessage("No changes — branch is identical to the default branch.");
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

// ── Agents View ───────────────────────────────────────────────────────

class AgentsView extends ItemView {
	private tickets: Ticket[] = [];
	private cronAgents: CronAgentRow[] = [];
	private previewLeaf: WorkspaceLeaf | null = null;
	private longPressTriggered = false;

	getViewType(): string {
		return AGENTS_VIEW_TYPE;
	}

	getDisplayText(): string {
		return "Tickets Agents";
	}

	getIcon(): string {
		return "bot";
	}

	async onOpen() {
		await this.refresh();

		this.registerEvent(this.app.vault.on("create", () => this.refresh()));
		this.registerEvent(this.app.vault.on("delete", () => this.refresh()));
		this.registerEvent(this.app.vault.on("rename", () => this.refresh()));
		this.registerEvent(this.app.vault.on("modify", (f) => {
			if (f instanceof TFile && (f.extension === "md" || f.path === CONFIG_PATH || isCronRunPath(f.path))) {
				this.refresh();
			}
		}));
	}

	private async refresh() {
		const config = await loadConfig(this.app);
		if (!config) {
			this.contentEl.empty();
			this.contentEl.createEl("p", {
				text: "Could not load config.yml — is this a tickets-md vault?",
				cls: "tb-error",
			});
			return;
		}

		const all = await loadTickets(this.app, config.stages);
		this.tickets = all
			.filter((t) => t.agent_status && AGENTS_VIEW_STATUSES.includes(t.agent_status))
			.sort((a, b) => {
				const au = a.updated_at ?? "";
				const bu = b.updated_at ?? "";
				if (au !== bu) return au < bu ? 1 : -1;
				return a.id.localeCompare(b.id, undefined, { numeric: true });
			});
		this.cronAgents = await Promise.all(
			(config.cron_agents ?? []).map(async (cron) => ({
				config: cron,
				lastRun: await loadLatestCronRun(this.app, cron.name),
			})),
		);
		this.cronAgents.sort((a, b) => a.config.name.localeCompare(b.config.name));
		this.render();
	}

	private render() {
		const container = this.contentEl;
		container.empty();
		container.addClass("tb-agents-container");

		const header = container.createDiv({ cls: "tb-header" });
		header.createEl("h2", { text: "Agents", cls: "tb-board-title" });
		const actions = header.createDiv({ cls: "tb-header-actions" });
		const totalCount = this.tickets.length + this.cronAgents.length;
		const count = actions.createEl("span", {
			text: String(totalCount),
			cls: "tb-count",
		});
		count.setAttribute("aria-label", `${totalCount} agent rows`);
		const refreshBtn = actions.createEl("button", {
			cls: "tb-header-btn",
			attr: { "aria-label": "Refresh agents" },
		});
		refreshBtn.textContent = "\u21BB";
		refreshBtn.addEventListener("click", () => this.refresh());

		if (totalCount === 0) {
			container.createDiv({ cls: "tb-empty", text: "No active agents or cron agents" });
			return;
		}

		if (this.tickets.length > 0) {
			const section = container.createDiv({ cls: "tb-agents-section" });
			const sectionHeader = section.createDiv({ cls: "tb-agents-section-header" });
			sectionHeader.createEl("h3", { text: "Tickets", cls: "tb-agents-section-title" });
			const list = section.createDiv({ cls: "tb-agents-list" });
			for (const ticket of this.tickets) {
				this.renderRow(list, ticket);
			}
		}

		if (this.cronAgents.length > 0) {
			const section = container.createDiv({ cls: "tb-agents-section tb-crons-section" });
			const sectionHeader = section.createDiv({ cls: "tb-agents-section-header" });
			sectionHeader.createEl("h3", { text: "Crons", cls: "tb-agents-section-title" });
			const list = section.createDiv({ cls: "tb-agents-list" });
			for (const cron of this.cronAgents) {
				this.renderCronRow(list, cron);
			}
		}
	}

	private renderRow(parent: HTMLElement, ticket: Ticket) {
		const row = parent.createDiv({ cls: "tb-agent-row" });

		row.addEventListener("click", async () => {
			if (this.longPressTriggered) {
				this.longPressTriggered = false;
				return;
			}
			this.previewLeaf = openPreviewLeaf(this.app, this.previewLeaf);
			await this.previewLeaf.openFile(ticket.file);
		});

		row.addEventListener("contextmenu", (e) => {
			e.preventDefault();
			this.buildRowMenu(ticket).showAtMouseEvent(e);
		});
		if (Platform.isMobile) {
			this.onLongPress(row, (x, y) => {
				this.buildRowMenu(ticket).showAtPosition({ x, y });
			});
		}

		const left = row.createDiv({ cls: "tb-agent-row-left" });
		if (ticket.agent_status && AGENT_BADGES[ticket.agent_status]) {
			const badge = AGENT_BADGES[ticket.agent_status];
			left.createEl("span", {
				text: badge.icon,
				cls: `tb-agent-badge ${badge.cls}`,
				attr: { "aria-label": ticket.agent_status },
			});
		}
		left.createEl("span", { text: ticket.id, cls: "tb-ticket-id" });
		left.createEl("span", { text: ticket.title, cls: "tb-agent-row-title" });

		const right = row.createDiv({ cls: "tb-agent-row-right" });
		right.createEl("span", { text: ticket.stage, cls: "tb-stage-name" });
		if (ticket.agent_session) {
			right.createEl("span", {
				text: ticket.agent_session,
				cls: "tb-agent-session",
			});
		}
		if (ticket.updated_at) {
			right.createEl("time", {
				text: ticket.updated_at,
				cls: "tb-agent-updated",
				attr: { datetime: ticket.updated_at },
			});
		}
	}

	private buildRowMenu(ticket: Ticket): Menu {
		const menu = new Menu();
		menu.addItem((item) =>
			item.setTitle("Open ticket").setIcon("file-text").onClick(async () => {
				this.previewLeaf = openPreviewLeaf(this.app, this.previewLeaf);
				await this.previewLeaf.openFile(ticket.file);
			}),
		);
		if (ticket.agent_session) {
			menu.addItem((item) =>
				item.setTitle("Open terminal").setIcon("terminal-square").onClick(() => {
					openTerminal(this.app, ticket);
				}),
			);
		}
		if (!Platform.isMobile && canRerunStageAgent(ticket)) {
			menu.addItem((item) =>
				item.setTitle("Re-run stage agent").setIcon("refresh-cw").onClick(() => {
					rerunStageAgent(this.app, ticket);
				}),
			);
		}
		menu.addItem((item) =>
			item.setTitle("View diff").setIcon("git-compare").onClick(() => {
				openDiff(this.app, ticket);
			}),
		);
		return menu;
	}

	private renderCronRow(parent: HTMLElement, cron: CronAgentRow) {
		const row = parent.createDiv({
			cls: `tb-agent-row tb-cron-row${cronAgentEnabled(cron.config) ? "" : " tb-cron-disabled"}`,
		});

		row.addEventListener("contextmenu", (e) => {
			e.preventDefault();
			this.buildCronMenu(cron).showAtMouseEvent(e);
		});
		if (Platform.isMobile) {
			this.onLongPress(row, (x, y) => {
				this.buildCronMenu(cron).showAtPosition({ x, y });
			});
		}

		const left = row.createDiv({ cls: "tb-agent-row-left" });
		if (cron.lastRun?.status && AGENT_BADGES[cron.lastRun.status]) {
			const badge = AGENT_BADGES[cron.lastRun.status];
			left.createEl("span", {
				text: badge.icon,
				cls: `tb-agent-badge ${badge.cls}`,
				attr: { "aria-label": cron.lastRun.status },
			});
		} else {
			left.createEl("span", {
				text: "\u2014",
				cls: "tb-agent-badge tb-agent-updated",
				attr: { "aria-label": "no runs" },
			});
		}
		left.createEl("span", { text: cron.config.name, cls: "tb-ticket-id" });
		left.createEl("span", { text: cron.config.command, cls: "tb-agent-row-title" });

		const meta = left.createDiv({ cls: "tb-cron-meta" });
		meta.createEl("span", { text: cron.config.schedule, cls: "tb-cron-schedule" });
		meta.createEl("span", {
			text: cronAgentEnabled(cron.config) ? "enabled" : "disabled",
			cls: `tb-cron-enabled${cronAgentEnabled(cron.config) ? "" : " tb-cron-enabled-off"}`,
		});

		const right = row.createDiv({ cls: "tb-agent-row-right" });
		right.createEl("span", {
			text: cron.lastRun?.run_id ?? "\u2014",
			cls: "tb-agent-session",
		});
		if (cron.lastRun?.status) {
			right.createEl("span", {
				text: cron.lastRun.status,
				cls: "tb-stage-name",
			});
		}
		right.createEl("span", {
			text: formatElapsedSince(cron.lastRun?.spawned_at),
			cls: "tb-agent-updated",
		});
	}

	private buildCronMenu(cron: CronAgentRow): Menu {
		const menu = new Menu();
		menu.addItem((item) =>
			item.setTitle("Edit config").setIcon("settings").onClick(() => {
				new CronAgentModal(this.app, parseCronAgentConfig(cron.config), async (updated) => {
					await this.saveCronAgent(cron.config.name, updated);
					new Notice("Saved cron agent config. Restart `tickets watch` to reload schedules.");
				}).open();
			}),
		);
		if (!Platform.isMobile) {
			menu.addItem((item) =>
				item.setTitle("Run now").setIcon("play").onClick(async () => {
					await runCronAgent(this.app, cron.config.name);
					await this.refresh();
				}),
			);
		}
		menu.addItem((item) =>
			item.setTitle(cronAgentEnabled(cron.config) ? "Disable" : "Enable")
				.setIcon(cronAgentEnabled(cron.config) ? "pause-circle" : "play-circle")
				.onClick(async () => {
					await this.saveCronAgent(cron.config.name, {
						...parseCronAgentConfig(cron.config),
						enabled: !cronAgentEnabled(cron.config),
					});
					new Notice("Updated cron agent. Restart `tickets watch` to apply schedule changes.");
				}),
		);
		const lastRun = cron.lastRun;
		if (lastRun?.run_id) {
			menu.addItem((item) =>
				item.setTitle("Open last run log").setIcon("file-text").onClick(async () => {
					await this.openCronLog(cron.config.name, lastRun);
				}),
			);
		}
		return menu;
	}

	private async saveCronAgent(name: string, updated: EditableCronAgentConfig): Promise<void> {
		const validationError = validateCronAgent(updated);
		if (validationError) {
			new Notice(validationError);
			return;
		}

		const config = await loadConfig(this.app);
		if (!config) {
			new Notice("Could not load config.yml");
			return;
		}

		const cronAgents = [...(config.cron_agents ?? [])];
		const index = cronAgents.findIndex((cron) => cron.name === name);
		if (index < 0) {
			new Notice(`Could not find cron agent ${name}`);
			return;
		}

		const next: CronAgentConfig = {
			...cronAgents[index],
			name: cronAgents[index].name,
			schedule: updated.schedule.trim(),
			command: updated.command.trim(),
			args: updated.args.split(",").map((arg) => arg.trim()).filter(Boolean),
			prompt: updated.prompt.trim(),
			enabled: updated.enabled,
		};
		if (cronAgentEntriesEqual(cronAgents[index], next)) {
			await this.refresh();
			return;
		}
		cronAgents[index] = next;
		config.cron_agents = cronAgents;
		await writeConfig(this.app, config);
		await this.refresh();
	}

	private async openCronLog(name: string, run: AgentRun): Promise<void> {
		if (!run.run_id) {
			new Notice("Cron run is missing a run_id");
			return;
		}
		const path = cronLogPath(name, run.run_id);
		const file = this.app.vault.getAbstractFileByPath(path);
		if (file instanceof TFile) {
			this.previewLeaf = openPreviewLeaf(this.app, this.previewLeaf);
			await this.previewLeaf.openFile(file);
			return;
		}

		if (run.log_file) {
			new Notice(`Cron log is outside the vault: ${run.log_file}`);
			return;
		}
		new Notice(`Could not find cron log at ${path}`);
	}

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
			if (this.longPressTriggered) {
				setTimeout(() => { this.longPressTriggered = false; }, 0);
			}
		});
		el.addEventListener("touchcancel", () => {
			if (timeout) { clearTimeout(timeout); timeout = null; }
			this.longPressTriggered = false;
		});
	}

	async onClose() {}
}

class ProjectsView extends ItemView {
	private config: TicketsConfig | null = null;
	private projects: Project[] = [];
	private tickets: Ticket[] = [];
	private selectedProjectId: string | null = null;
	private previewLeaf: WorkspaceLeaf | null = null;
	private longPressTriggered = false;

	getViewType(): string {
		return PROJECTS_VIEW_TYPE;
	}

	getDisplayText(): string {
		return "Tickets Projects";
	}

	getIcon(): string {
		return "folder-open";
	}

	async onOpen() {
		await this.refresh();

		this.registerEvent(this.app.vault.on("create", () => this.refresh()));
		this.registerEvent(this.app.vault.on("delete", () => this.refresh()));
		this.registerEvent(this.app.vault.on("rename", () => this.refresh()));
		this.registerEvent(this.app.vault.on("modify", (f) => {
			if (f instanceof TFile && (f.extension === "md" || f.path === CONFIG_PATH)) {
				this.refresh();
			}
		}));
	}

	private async refresh() {
		const config = await loadConfig(this.app);
		if (!config) {
			this.contentEl.empty();
			this.contentEl.createEl("p", {
				text: "Could not load config.yml — is this a tickets-md vault?",
				cls: "tb-error",
			});
			return;
		}

		this.config = config;
		this.projects = await loadProjects(this.app, config);
		this.tickets = await loadTickets(this.app, config.stages);
		if (this.selectedProjectId && !this.projects.some((project) => project.id === this.selectedProjectId)) {
			this.selectedProjectId = null;
		}
		if (!this.selectedProjectId && this.projects.length > 0) {
			this.selectedProjectId = this.projects[0].id;
		}
		this.render();
	}

	private render() {
		const container = this.contentEl;
		container.empty();
		container.addClass("tb-projects-container");

		const header = container.createDiv({ cls: "tb-header" });
		header.createEl("h2", { text: "Projects", cls: "tb-board-title" });
		const actions = header.createDiv({ cls: "tb-header-actions" });
		const count = actions.createEl("span", {
			text: String(this.projects.length),
			cls: "tb-count",
		});
		count.setAttribute("aria-label", `${this.projects.length} projects`);

		const refreshBtn = actions.createEl("button", {
			cls: "tb-header-btn",
			attr: { "aria-label": "Refresh projects" },
		});
		refreshBtn.textContent = "\u21BB";
		refreshBtn.addEventListener("click", () => this.refresh());

		const createBtn = actions.createEl("button", {
			cls: "tb-header-btn",
			text: "+ New project",
			attr: { "aria-label": "Create project" },
		});
		createBtn.addClass("tb-projects-create-btn");
		createBtn.addEventListener("click", () => this.createProject());

		if (this.projects.length === 0) {
			container.createDiv({ cls: "tb-empty", text: "No projects yet" });
			return;
		}

		const body = container.createDiv({ cls: "tb-projects-body" });
		const list = body.createDiv({ cls: "tb-projects-list" });
		for (const project of this.projects) {
			this.renderRow(list, project);
		}
		this.renderTicketsSidebar(body);
	}

	private renderRow(parent: HTMLElement, project: Project) {
		const assignedCount = this.tickets.filter((ticket) => ticket.project === project.id).length;
		const row = parent.createDiv({ cls: "tb-project-row" });
		if (project.id === this.selectedProjectId) {
			row.addClass("tb-project-row-selected");
		}

		row.addEventListener("click", () => {
			if (this.longPressTriggered) {
				this.longPressTriggered = false;
				return;
			}
			if (this.selectedProjectId === project.id) {
				return;
			}
			this.selectedProjectId = project.id;
			this.render();
		});

		row.addEventListener("contextmenu", (e) => {
			e.preventDefault();
			this.buildRowMenu(project).showAtMouseEvent(e);
		});
		if (Platform.isMobile) {
			this.onLongPress(row, (x, y) => {
				this.buildRowMenu(project).showAtPosition({ x, y });
			});
		}

		const left = row.createDiv({ cls: "tb-project-row-left" });
		const meta = left.createDiv({ cls: "tb-project-row-meta" });
		meta.createEl("span", { text: project.id, cls: "tb-ticket-id" });
		if (project.status) {
			meta.createEl("span", { text: project.status, cls: "tb-project-status" });
		}
		left.createEl("span", { text: project.title, cls: "tb-project-row-title" });

		const right = row.createDiv({ cls: "tb-project-row-right" });
		right.createEl("span", {
			text: `${assignedCount} ticket${assignedCount === 1 ? "" : "s"}`,
			cls: "tb-project-count",
		});
		if (project.updated_at) {
			right.createEl("time", {
				text: project.updated_at,
				cls: "tb-agent-updated",
				attr: { datetime: project.updated_at },
			});
		}
	}

	private renderTicketsSidebar(parent: HTMLElement) {
		const sidebar = parent.createDiv({ cls: "tb-project-tickets" });
		const selectedProject = this.projects.find((project) => project.id === this.selectedProjectId) ?? null;
		if (!selectedProject) {
			sidebar.createDiv({ cls: "tb-empty", text: "Select a project to see its tickets" });
			return;
		}

		const assigned = this.tickets
			.filter((ticket) => ticket.project === selectedProject.id)
			.sort((a, b) => a.id.localeCompare(b.id, undefined, { numeric: true }));

		const header = sidebar.createDiv({ cls: "tb-project-tickets-header" });
		const titleGroup = header.createDiv({ cls: "tb-project-tickets-title" });
		titleGroup.createEl("span", { text: selectedProject.id, cls: "tb-ticket-id" });
		titleGroup.createEl("span", {
			text: `${assigned.length} ticket${assigned.length === 1 ? "" : "s"}`,
			cls: "tb-project-count",
		});
		header.createEl("div", { text: selectedProject.title, cls: "tb-project-tickets-name" });
		const openBtn = header.createEl("button", {
			text: "Open project",
			cls: "tb-project-open-btn",
			attr: { "aria-label": `Open ${selectedProject.id}` },
		});
		openBtn.addEventListener("click", async () => {
			try {
				this.previewLeaf = openPreviewLeaf(this.app, this.previewLeaf);
				await this.previewLeaf.openFile(selectedProject.file);
			} catch (err) {
				new Notice(`Could not open ${selectedProject.id}: ${err instanceof Error ? err.message : String(err)}`);
			}
		});

		if (assigned.length === 0) {
			sidebar.createDiv({ cls: "tb-empty", text: "No tickets assigned" });
			return;
		}

		const list = sidebar.createDiv({ cls: "tb-project-ticket-list" });
		for (const ticket of assigned) {
			const row = list.createDiv({ cls: "tb-project-ticket-row" });
			row.addEventListener("click", async () => {
				try {
					this.previewLeaf = openPreviewLeaf(this.app, this.previewLeaf);
					await this.previewLeaf.openFile(ticket.file);
				} catch (err) {
					new Notice(`Could not open ${ticket.id}: ${err instanceof Error ? err.message : String(err)}`);
				}
			});

			const meta = row.createDiv({ cls: "tb-project-ticket-meta" });
			meta.createEl("span", { text: ticket.id, cls: "tb-ticket-id" });
			meta.createEl("span", { text: ticket.stage, cls: "tb-stage-name" });
			if (ticket.priority) {
				meta.createEl("span", {
					text: ticket.priority,
					cls: `tb-priority tb-priority-${ticket.priority.toLowerCase()}`,
				});
			}
			if (ticket.agent_status && AGENT_BADGES[ticket.agent_status]) {
				const badge = AGENT_BADGES[ticket.agent_status];
				meta.createEl("span", {
					text: badge.icon,
					cls: `tb-agent-badge ${badge.cls}`,
					attr: { "aria-label": ticket.agent_status },
				});
			}
			row.createEl("div", { text: ticket.title, cls: "tb-project-ticket-title" });
		}
	}

	private buildRowMenu(project: Project): Menu {
		const menu = new Menu();
		menu.addItem((item) =>
			item.setTitle("Open project").setIcon("file-text").onClick(async () => {
				this.previewLeaf = openPreviewLeaf(this.app, this.previewLeaf);
				await this.previewLeaf.openFile(project.file);
			}),
		);
		menu.addItem((item) =>
			item.setTitle("Rename").setIcon("pencil").onClick(() => {
				new TextInputModal(
					this.app,
					"Rename project",
					"Project title",
					project.title,
					async (title) => {
						await updateFrontmatterFile(this.app, project.file, (fm) => {
							fm.title = title;
						});
						await this.refresh();
					},
				).open();
			}),
		);
		menu.addItem((item) =>
			item.setTitle("Set status").setIcon("circle-dot").onClick(() => {
				new TextInputModal(
					this.app,
					"Project status",
					"active",
					project.status ?? "",
					async (status) => {
						await updateFrontmatterFile(this.app, project.file, (fm) => {
							fm.status = status;
						});
						await this.refresh();
					},
					true,
				).open();
			}),
		);
		menu.addItem((item) =>
			item.setTitle("Assign tickets...").setIcon("folder-plus").onClick(() => {
				const candidates = this.tickets
					.filter((ticket) => ticket.project !== project.id)
					.sort((a, b) => a.id.localeCompare(b.id, undefined, { numeric: true }));
				if (candidates.length === 0) {
					new Notice(`No tickets available to assign to ${project.id}`);
					return;
				}
				new FuzzyPickerModal<Ticket>(
					this.app,
					candidates,
					(ticket) => {
						let label = `${ticket.id} — ${ticket.title} (${ticket.stage})`;
						if (ticket.project) {
							label += ` · currently ${ticket.project}`;
						}
						return label;
					},
					async (ticket) => {
						try {
							await updateFrontmatterFile(this.app, ticket.file, (fm) => {
								fm.project = project.id;
							});
							await this.refresh();
						} catch (err) {
							new Notice(`Could not assign ${ticket.id}: ${err instanceof Error ? err.message : String(err)}`);
						}
					},
				).open();
			}),
		);
		menu.addItem((item) =>
			item.setTitle("Delete project").setIcon("trash").onClick(() => {
				new ConfirmProjectDeleteModal(this.app, project, async () => {
					const failures: string[] = [];
					for (const ticket of this.tickets.filter((candidate) => candidate.project === project.id)) {
						try {
							await updateFrontmatterFile(this.app, ticket.file, (fm) => {
								delete fm.project;
							});
						} catch {
							failures.push(ticket.id);
						}
					}
					await this.app.vault.trash(project.file, false);
					if (this.selectedProjectId === project.id) {
						this.selectedProjectId = null;
					}
					if (failures.length > 0) {
						new Notice(`Deleted ${project.id} but could not clear assignment on: ${failures.join(", ")}`);
					}
					await this.refresh();
				}).open();
			}),
		);
		return menu;
	}

	private async createProject() {
		if (!this.config) return;

		new TextInputModal(
			this.app,
			"New project",
			"Project title",
			"",
			async (title) => {
				try {
					await this.ensureProjectsDir();
					for (let attempt = 0; attempt < 1000; attempt++) {
						const id = await nextProjectId(this.app, this.config!);
						const path = `${PROJECTS_DIR}/${id}.md`;
						if (this.app.vault.getAbstractFileByPath(path)) {
							continue;
						}
						const timestamp = nowTimestamp();
						const content = serializeFrontmatterContent(
							{
								id,
								title,
								created_at: timestamp,
								updated_at: timestamp,
							},
							"\n\n## Description\n\n_Describe the project here._\n",
						);
						try {
							const file = await this.app.vault.create(path, content);
							this.previewLeaf = openPreviewLeaf(this.app, this.previewLeaf);
							await this.previewLeaf.openFile(file);
							await this.refresh();
							return;
						} catch (error) {
							if (error instanceof Error && error.message.includes("already exists")) {
								continue;
							}
							throw error;
						}
					}
					new Notice("Could not allocate a new project ID");
				} catch (error) {
					new Notice(error instanceof Error ? error.message : "Could not create project");
				}
			},
		).open();
	}

	private async ensureProjectsDir() {
		const existing = this.app.vault.getAbstractFileByPath(PROJECTS_DIR);
		if (existing instanceof TFolder) return;
		if (existing instanceof TFile) {
			throw new Error(`${PROJECTS_DIR} already exists as a file`);
		}
		await this.app.vault.createFolder(PROJECTS_DIR);
	}

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
			if (this.longPressTriggered) {
				setTimeout(() => { this.longPressTriggered = false; }, 0);
			}
		});
		el.addEventListener("touchcancel", () => {
			if (timeout) { clearTimeout(timeout); timeout = null; }
			this.longPressTriggered = false;
		});
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

class CronAgentModal extends Modal {
	private config: EditableCronAgentConfig;
	private readonly onSave: (config: EditableCronAgentConfig) => Promise<void>;

	constructor(
		app: import("obsidian").App,
		config: EditableCronAgentConfig,
		onSave: (config: EditableCronAgentConfig) => Promise<void>,
	) {
		super(app);
		this.config = { ...config };
		this.onSave = onSave;
	}

	onOpen() {
		const { contentEl } = this;
		this.modalEl.addClass("tb-config-modal");

		contentEl.createEl("h3", { text: `${this.config.name} — Cron Config` });

		new Setting(contentEl)
			.setName("Name")
			.setDesc("Cron agent identifier")
			.addText((text) => {
				text.setValue(this.config.name);
				text.inputEl.disabled = true;
				return text;
			});

		new Setting(contentEl)
			.setName("Schedule")
			.setDesc("Cron expression, e.g. @every 5m")
			.addText((text) =>
				text
					.setPlaceholder("@every 5m")
					.setValue(this.config.schedule)
					.onChange((v) => (this.config.schedule = v)),
			);

		new Setting(contentEl)
			.setName("Command")
			.setDesc("CLI binary to invoke")
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

		new Setting(contentEl)
			.setName("Enabled")
			.setDesc("Whether `tickets watch` should schedule this cron")
			.addToggle((toggle) =>
				toggle
					.setValue(this.config.enabled)
					.onChange((v) => (this.config.enabled = v)),
			);

		contentEl.createEl("div", {
			text: "Prompt",
			cls: "setting-item-name tb-prompt-label",
		});
		contentEl.createEl("div", {
			text: "Template sent to the agent each time this cron fires",
			cls: "setting-item-description tb-prompt-desc",
		});
		const promptArea = contentEl.createEl("textarea", {
			cls: "tb-config-editor",
		});
		promptArea.value = this.config.prompt;
		promptArea.spellcheck = false;
		promptArea.placeholder = "Review the backlog and propose the next ticket moves.";
		promptArea.addEventListener("input", () => {
			this.config.prompt = promptArea.value;
		});

		new Setting(contentEl).addButton((btn) =>
			btn.setButtonText("Save").setCta().onClick(async () => {
				const validationError = validateCronAgent(this.config);
				if (validationError) {
					new Notice(validationError);
					return;
				}
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

class ConfirmProjectDeleteModal extends Modal {
	private readonly project: Project;
	private readonly onConfirm: () => Promise<void>;

	constructor(
		app: import("obsidian").App,
		project: Project,
		onConfirm: () => Promise<void>,
	) {
		super(app);
		this.project = project;
		this.onConfirm = onConfirm;
	}

	onOpen() {
		const { contentEl } = this;
		this.modalEl.addClass("tb-confirm-delete-modal");

		contentEl.createEl("h3", { text: "Delete project" });
		contentEl.createEl("p", {
			text: `Are you sure you want to delete ${this.project.id} (${this.project.title})? Assigned tickets will be unassigned first.`,
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
	private readonly onSubmit: (value: string) => void | Promise<void>;
	private readonly allowEmpty: boolean;

	constructor(
		app: import("obsidian").App,
		title: string,
		placeholder: string,
		defaultValue: string,
		onSubmit: (value: string) => void | Promise<void>,
		allowEmpty = false,
	) {
		super(app);
		this.title = title;
		this.placeholder = placeholder;
		this.defaultValue = defaultValue;
		this.result = defaultValue;
		this.onSubmit = onSubmit;
		this.allowEmpty = allowEmpty;
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
			btn.setButtonText("Confirm").setCta().onClick(async () => {
				const trimmed = this.result.trim();
				if (trimmed || this.allowEmpty) {
					await this.onSubmit(trimmed);
				}
				this.close();
			}),
		);
	}

	onClose() {
		this.contentEl.empty();
	}
}

class FuzzyPickerModal<T> extends FuzzySuggestModal<T> {
	private readonly items: T[];
	private readonly getText: (item: T) => string;
	private readonly onChoose: (item: T) => void;

	constructor(
		app: import("obsidian").App,
		items: T[],
		getText: (item: T) => string,
		onChoose: (item: T) => void,
	) {
		super(app);
		this.items = items;
		this.getText = getText;
		this.onChoose = onChoose;
	}

	getItems(): T[] {
		return this.items;
	}

	getItemText(item: T): string {
		return this.getText(item);
	}

	onChooseItem(item: T): void {
		this.onChoose(item);
	}
}
