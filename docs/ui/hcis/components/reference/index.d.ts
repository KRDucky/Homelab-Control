// HCIS — Homelab Control Interface System. Types as documentation (not type-checked).
// Global: window.HCIS. Requires React 18+ (the app uses React 19; both work).
import type { ReactNode } from 'react';

export type Tone = 'ok' | 'warn' | 'crit' | 'info' | 'unknown' | 'neutral' | 'accent';
export type Density = 'compact' | 'standard' | 'spacious'; // set via data-hc-density, a layout preference
export type IconName = 'overview' | 'health' | 'system' | 'storage' | 'containers' | 'vm' | 'network' | 'share' | 'packages' | 'hardware' | 'audit' | 'settings' | 'search' | 'bell' | 'activity' | 'chevron-right' | 'chevron-down' | 'chevron-left' | 'check' | 'warning' | 'critical' | 'info' | 'unknown' | 'play' | 'stop' | 'restart' | 'more' | 'sort' | 'sort-asc' | 'sort-desc' | 'folder' | 'file' | 'lock' | 'user' | 'terminal' | 'copy' | 'filter' | 'plus' | 'close' | 'menu' | 'sidebar' | 'server' | 'drive' | 'locate' | 'history' | 'logs' | 'wrench' | 'shield' | 'external' | 'host' | 'download' | 'key' | 'pause' | 'eye';

/* Production (ported from frontend/app/src/ui.tsx and Meter.tsx) */
export interface SectionMeta { status: 'ok' | 'partial' | 'unavailable'; sources: string[]; errors?: string[]; notes?: string[] }
export interface SectionProps { title: string; meta?: SectionMeta; children?: ReactNode; id?: string; actions?: ReactNode; level?: 2 | 3; live?: LiveIndicatorProps; glass?: boolean }
export interface StateDotProps { tone: 'ok' | 'warn' | 'crit' | 'info' | 'idle'; pulse?: boolean }
export interface MeterProps { label: string; used: number | undefined; total: number | undefined; detail?: string; thresholds?: { warn: number; crit: number }; compact?: boolean; hideHead?: boolean }

/* Actions */
export interface ButtonProps { variant?: 'primary' | 'secondary' | 'ghost' | 'danger'; size?: 'sm' | 'md'; icon?: IconName; busy?: boolean; disabledReason?: string; onClick?: () => void; type?: 'button' | 'submit'; children: ReactNode }
export interface IconButtonProps { icon: IconName; label: string; variant?: 'ghost' | 'secondary' | 'primary'; onClick?: () => void }
export interface MenuItem { id?: string; label: string; icon?: IconName; shortcut?: string; danger?: boolean; onSelect?: () => void }
export interface DropdownProps { label: string; items: (MenuItem | '-')[]; icon?: IconName; iconOnly?: boolean; align?: 'start' | 'end'; variant?: 'secondary' | 'ghost' | 'primary' }

/* Forms */
export interface TextFieldProps extends React.InputHTMLAttributes<HTMLInputElement> { label?: ReactNode; help?: string; error?: string; mono?: boolean; icon?: IconName }
export interface SearchFieldProps extends React.InputHTMLAttributes<HTMLInputElement> { label?: string; shortcut?: string }
export interface SelectProps extends React.SelectHTMLAttributes<HTMLSelectElement> { label?: string; help?: string; options: (string | { value: string; label: string })[] }
export interface CheckboxProps extends React.InputHTMLAttributes<HTMLInputElement> { label: ReactNode; indeterminate?: boolean }
export interface SwitchProps { label: string; checked: boolean; onChange: (v: boolean) => void; disabled?: boolean }
export interface SegmentedControlProps { legend: string; name?: string; value: string; onChange: (id: string) => void; options: { id: string; label: string; icon?: IconName; count?: number }[] }
export interface PermissionEditorProps { value: string; onChange?: (octal: string) => void; compact?: boolean } // e.g. '0755'
export interface FileEntry { name: string; dir?: boolean; size: string; bytes?: number; owner: string; mode: string; modified: string }
export interface FolderNode { name: string; children?: FolderNode[] }
export interface FileBrowserProps { tree: FolderNode[]; files: FileEntry[]; path: string; onOpen?: (path: string) => void }

/* Status */
export interface StatusPillProps { tone?: Tone; label: string; detail?: string; icon?: IconName | null } // always text; colour never alone
export interface BadgeProps { tone?: 'ok' | 'warn' | 'crit' | 'neutral'; children?: ReactNode; count?: number }
export interface BannerProps { tone?: 'info' | 'ok' | 'warn' | 'crit'; title?: string; children: ReactNode; action?: ReactNode; role?: 'status' | 'alert' }
export interface CheckListProps { items: { id: string | number; level: 'ok' | 'warn' | 'crit' | 'info'; message: ReactNode; action?: ReactNode }[] }
export interface ConfidenceProps { level: 'verified' | 'inferred' | 'unknown' }

/* Data */
export interface DataTableColumn<Row> { id: string; header: ReactNode; cell?: (row: Row) => ReactNode; sortValue?: (row: Row) => string | number; sortable?: boolean; numeric?: boolean; wrap?: boolean }
export interface DataTableProps<Row> { caption: string; captionHidden?: boolean; columns: DataTableColumn<Row>[]; rows: readonly Row[]; rowKey: (row: Row) => string; selectable?: boolean; onSelectionChange?: (keys: string[]) => void; bulkActions?: (keys: string[]) => ReactNode; rowTone?: (row: Row) => 'crit' | null; initialSort?: { id: string; dir: 'asc' | 'desc' }; emptyState?: ReactNode }
export interface KeyValueListProps { items: { term: string; value: ReactNode; mono?: boolean }[]; layout?: 'grid' | 'rows' }
export interface ResourceCardProps { icon?: IconName; title: string; subtitle?: string; status?: ReactNode; metric?: ReactNode; unit?: string; href?: string; onClick?: () => void; children?: ReactNode }
export interface SparklineProps { values: number[]; label: string }
export interface MetricChartProps { title: string; labels: string[]; series: { name: string; values: number[] }[]; unit?: string; max?: number; height?: number }
export interface ProgressBarProps { label: string; value?: number; max?: number; indeterminate?: boolean }

/* Navigation & shell */
export interface BreadcrumbsProps { items: { label: string; href?: string }[] }
export interface TabsProps { tabs: { id: string; label: string; icon?: IconName; badge?: number }[]; value?: string; onChange?: (id: string) => void; label?: string; children: ReactNode | ((current: string) => ReactNode) }
export interface ResourceHeaderProps { kind: string; name: string; icon?: IconName; status?: ReactNode; facts?: KeyValueListProps['items']; actions?: ReactNode }
export interface NavGroup { group: string | null; items: { id: string; label: string; icon: IconName }[] }
export interface SidebarProps { nav?: NavGroup[]; current?: string; onNavigate?: (id: string) => void; counts?: Record<string, ReactNode>; unavailable?: Record<string, string>; hideUnavailable?: boolean; rail?: boolean }
export interface Operation { id: string | number; title: string; state: 'queued' | 'running' | 'succeeded' | 'failed' | 'cancelled' }
export interface GlobalHeaderProps { hosts: string[]; host: string; onHost?: (h: string) => void; onToggleSidebar?: () => void; collapsed?: boolean; onMenu?: () => void; onSearch?: () => void; notifications?: number; operations?: Operation[]; user?: string; health?: { tone: 'ok' | 'warn' | 'crit' | 'unknown'; label: string }; onHealth?: () => void; prefs?: AppearancePrefs; onPrefs?: (p: AppearancePrefs) => void; workspaces?: Workspace[]; workspace?: string; onWorkspace?: (id: string) => void }
export interface AppShellProps extends Omit<SidebarProps, 'nav' | 'rail'> { hosts?: string[]; density?: Density; operations?: Operation[]; notifications?: number; onSearch?: () => void; health?: GlobalHeaderProps['health']; workspaces?: Workspace[]; initialPrefs?: Partial<AppearancePrefs>; applyThemeTo?: 'document' | 'none'; children: ReactNode }

/* Overlays */
export interface DialogProps { open: boolean; onClose: () => void; title?: string; icon?: ReactNode; footer?: ReactNode; children: ReactNode }
export interface ConfirmDialogProps { open: boolean; onClose: () => void; onConfirm: () => void; title: string; riskClass?: RiskClass; risk?: 'low' | 'medium' | 'high'; consequences?: string[]; confirmLabel?: string; typeToConfirm?: string; reauth?: boolean; children?: ReactNode }
export interface RiskCalloutProps { riskClass?: RiskClass; level?: 'low' | 'medium' | 'high'; title?: string; items?: string[] }
export interface Command { id: string; group: string; label: string; icon?: IconName; hint?: string; keywords?: string; kind?: 'navigate' | 'read' | 'operation'; run?: () => void }
export interface CommandPaletteProps { open: boolean; onClose: () => void; commands: Command[] }

/* Operations */
export interface LogLine { ts: string; level: 'err' | 'warn' | 'info' | 'debug'; msg: string }
export interface LogViewerProps { lines: LogLine[]; title?: string; follow?: boolean }
export interface AuditEvent { id: string | number; title: string; outcome?: 'ok' | 'crit' | 'warn' | 'info'; time: string; actor: string; scope: string; detail?: string }
export interface AuditTimelineProps { events: AuditEvent[] }
export interface JobProgressProps { title: string; state?: Operation['state']; steps?: { label: string; state: 'done' | 'run' | 'wait' | 'fail' }[]; done?: number; total?: number; started?: string; onCancel?: () => void; onOpen?: () => void; result?: { state: 'verified' | 'unverified' | 'failed'; summary: string; auditRef?: string } }
export interface PlanChange { op: 'add' | 'change' | 'remove' | 'skip'; label: string; diff?: string[] }
export interface OperationPreviewProps { title: string; riskClass?: RiskClass; risk?: 'low' | 'medium' | 'high'; target?: { canonical: string; items?: KeyValueListProps['items'] } | KeyValueListProps['items']; summary?: string; counts?: { label: string; value: ReactNode }[]; changes: PlanChange[]; dependencies?: string[]; consequences?: string[]; applyLabel?: string; onApply?: () => void; onCancel?: () => void }
export interface ResourceInspectorProps { kind: string; title: string; fields: KeyValueListProps['items']; raw: string; onClose?: () => void }

/* Domain views */
export interface ServiceDetailProps { unit: string; description: string; state: 'active' | 'inactive' | 'failed' | string; sub?: string; enabled: boolean; since: string; pid?: number; memory: string; exitCode?: string; restarts?: number; failure?: { category: string; confidence: 'confirmed' | 'suspected'; summary: string; evidence: string[] }; deps?: { unit: string; relation: string; state: string }[]; logs?: LogLine[]; unitFile?: string; steps?: ResolutionStep[]; docs?: { label: string; href: string }[]; history?: AuditEvent[] }
export interface HealthDashboardProps { findings: Finding[]; modules?: { name: string; state: 'ok' | 'warn' | 'crit' | 'unknown'; note?: string }[]; scans?: { time: string; checks: number; found: number; duration: string }[]; lastScan?: string; onScan?: () => void }
export interface HealthScoreProps { score: number; label?: string }
export interface Container { name: string; state: 'running' | 'exited' | 'restarting' | 'paused' | 'created'; image: string; ports?: string; cpu: number; mem: string; health?: string; restarts?: number }
export interface ContainerManagerProps { stacks: { name: string; path: string; containers: Container[] }[]; images?: { tag: string; id: string; size: string; used?: string; created: string }[]; networks?: { name: string; driver: string; subnet: string; containers: number }[]; volumes?: { name: string; driver: string; size: string; used?: string }[]; engine?: KeyValueListProps['items'] }
export interface SafePermissionsManagerProps { tree: FolderNode[]; directories: PermDir[]; savedLists?: { name: string; directories: PermDir[] }[]; users?: { name: string; id: number }[]; groups?: { name: string; id: number }[]; history?: AuditEvent[]; onExecute?: () => void }
export interface BaySlot { id: string; label: string; row: number; col: number; rowSpan?: number; colSpan?: number; type?: 'hdd' | 'ssd' | 'nvme'; reserved?: boolean }
export interface Drive { id: string; model: string; serial: string; size: string; type: 'hdd' | 'ssd' | 'nvme'; pool?: string | null; tone: 'ok' | 'warn' | 'crit'; health: string; temp: string; hba: string; port: string; confidence: 'verified' | 'inferred' | 'unknown'; dev: string; wwn: string; status?: 'present' | 'missing' | 'replaced'; prevSerial?: string; locating?: boolean }
export interface DriveLayout { id: string; name: string; chassis: string; rows: number; cols: number; slots: BaySlot[] }
export interface DriveBayGridProps { layouts: DriveLayout[]; drives: Drive[]; assignments: Record<string, string>; onLocate?: () => void } // export/import JSON: { hcisDriveLayout: 1, name, chassis, rows, cols, slots, assignments }
export interface Pool { name: string; kind: 'ZFS' | 'Btrfs' | 'mergerfs' | 'SnapRAID' | 'mdadm' | 'LVM2' | string; layout: string; state: string; tone: Tone; used: number; size: number; notes?: string; vdevs?: { name: string; tone: StateDotProps['tone']; note: string }[] }
export interface StoragePoolsProps { pools: Pool[] }
export interface VMListProps { vms: { name: string; state: 'running' | 'stopped' | 'paused' | 'crashed'; vcpu: number; ram: string; cpu?: number[]; ip?: string }[] }
export interface NetworkInterfacesProps { ifaces: { name: string; kind: string; state: 'up' | 'down'; speed?: string; addr: string[]; rx?: number[]; members?: string }[] }
export interface PackageListProps { packages: { name: string; installed: string; update?: string; security?: boolean; repo: string }[] }
export interface ShareListProps { shares: { name: string; proto: string[]; path: string; access?: string; guest?: boolean; state: string; tone: Tone }[] }
export interface IconProps { name: IconName; size?: 'sm' | 'md'; label?: string }

/* ---- HCIS v2 additions ---- */
export type RiskClass = 'read-only' | 'reversible' | 'disruptive' | 'destructive';
export interface AppearancePrefs { theme: 'midnight' | 'daybreak' | 'graphite' | 'oled' | string; transparency: 'full' | 'reduced'; density: Density; motion: 'full' | 'reduced' | 'none'; font?: 'inter' | 'system'; textScale?: '90' | '100' | '112' | '125' }
export interface Workspace { id: string; label: string; icon?: IconName }
export interface LiveIndicatorProps { state: 'live' | 'refreshing' | 'stale' | 'paused' | 'disconnected'; updated?: string; onRetry?: () => void }
export interface StatusIndicatorProps { state: 'ok' | 'warn' | 'crit' | 'info' | 'notice' | 'unknown'; label: string; detail?: string }
export interface FilterBarProps { query: string; onQuery: (q: string) => void; placeholder?: string; facets?: { id: string; label: string; value: string; options: string[] }[]; onFacet?: (id: string, value: string) => void; count?: number; total?: number; actions?: ReactNode }
export interface TreeNode { id: string; label: string; icon?: IconName; status?: StateDotProps['tone']; meta?: string; children?: TreeNode[] }
export interface ResourceTreeProps { nodes: TreeNode[]; selected?: string; onSelect?: (n: TreeNode) => void; label?: string; defaultExpanded?: string[] }
export interface DetailPanelProps { kind?: string; title: string; onClose?: () => void; actions?: ReactNode; floating?: boolean; children: ReactNode }
export interface MetricTileProps { label: string; value: ReactNode; unit?: string; tone?: 'ok' | 'warn' | 'crit' | 'info' | 'unknown'; trend?: number[]; detail?: string; icon?: IconName }
export interface CapacityBarProps { label: string; total: number; unit?: string; segments: { label: string; value: number; series?: 1 | 2 | 3 | 4 | 5 | 6 }[] }
export type TimeSeriesChartProps = MetricChartProps; // adds a generated text summary
export interface HealthSummaryProps { counts: { crit?: number; warn?: number; notice?: number; ok?: number; unknown?: number }; lastScan: string; scanning?: boolean; onScan?: () => void; unchecked?: number }
export interface DependencyGraphProps { levels: { kind: string; nodes: { id: string; label: string; icon?: IconName; status?: StateDotProps['tone'] }[] }[]; links: [string, string][]; selected?: string; onSelect?: (n: { id: string }) => void; label?: string }
export interface NumericInputProps { label: string; value: number | string; onChange: (v: number | string) => void; min?: number; max?: number; step?: number; unit?: string; help?: string; error?: string }
export interface ComboboxProps { label: string; options: { value: string; label: string; detail?: string }[]; value: string; onChange: (v: string) => void; placeholder?: string; help?: string; allowCustom?: (q: string) => { value: string; label: string; detail?: string } | null; mono?: boolean }
export interface RadioGroupProps { legend: string; options: { id: string; label: string; description?: string }[]; value: string; onChange: (id: string) => void; name?: string }
export interface PathPickerProps { label: string; value: string; onChange: (p: string) => void; tree: FolderNode[]; help?: string }
export interface UserGroupPickerProps { kind?: 'user' | 'group'; label: string; entries: { name: string; id: number }[]; value: string; onChange: (v: string) => void }
export interface AlertProps { tone?: 'info' | 'ok' | 'warn' | 'crit'; title?: string; children: ReactNode }
export interface ToastRegionProps { toasts: { id: string | number; tone?: 'info' | 'ok' | 'warn' | 'crit'; title: string; body?: string; action?: ReactNode }[]; onDismiss: (id: string | number) => void }
export interface EmptyStateProps { icon?: IconName; title: string; children?: ReactNode; action?: ReactNode }
export interface ErrorStateProps { title: string; children?: ReactNode; detail?: string; onRetry?: () => void }
export interface LoadingStateProps { label?: string; rows?: number }
export interface TooltipProps { text: string; label?: string; children?: ReactNode }
export interface RiskBadgeProps { riskClass: RiskClass }
export interface BackgroundJobPanelProps { jobs: JobProgressProps[]; title?: string }
export interface Finding { id: string; severity: 'crit' | 'warn' | 'notice'; confidence: 'confirmed' | 'suspected'; module: string; title: string; summary: string; firstSeen: string; lastSeen: string; evidence?: string[]; caveat?: string; investigate?: string[]; solution?: string; fix?: string; verification?: 'unverified' | 'resolved'; acknowledged?: boolean }
export interface DiagnosticFindingProps { f: Finding; onAck?: (id: string) => void; onSuppress?: (id: string) => void; defaultOpen?: boolean }
export interface ResolutionStep { kind?: 'investigate' | 'fix' | 'verify'; title: string; body?: string; command?: string }
export interface ResolutionGuideProps { title?: string; steps: ResolutionStep[]; docs?: { label: string; href: string }[] }
export interface TerminalViewerProps { title?: string; host?: string; lines: ({ prompt?: string; cmd: string } | { out: string; err?: boolean })[] }
export interface PermDir { path: string; usedBy: string; owner: string; mode: string; files: number; dirs: number; flags?: string[] }
export interface NavigationRailProps extends Omit<SidebarProps, 'rail'> {}
export interface AppearanceMenuProps { prefs: AppearancePrefs; onChange: (p: AppearancePrefs) => void; align?: 'start' | 'end' }
export interface WorkspaceSwitcherProps { workspaces: Workspace[]; current: string; onChange?: (id: string) => void; onSave?: () => void }
export interface OverviewDashboardProps { live?: LiveIndicatorProps['state']; updated?: string; metrics: MetricTileProps[]; chart: MetricChartProps; alerts: { tone: 'warn' | 'crit' | 'info'; title: string; body: string; action: string }[]; jobs: JobProgressProps[]; counts: HealthSummaryProps['counts']; onRetry?: () => void }
export interface StorageOverviewProps { pools: Pool[]; disks: { bay: string; model: string; serial: string; health: string; tone: Tone; temp: string; pool: string }[]; relations: { defaultSelected: string; levels: DependencyGraphProps['levels']; links: DependencyGraphProps['links'] } }
export interface ResourceExplorerProps { tree: TreeNode[]; resources: Record<string, { kind: string; label: string; path?: string[]; props: KeyValueListProps['items']; related?: { id: string; label: string; icon: IconName; relation: string }[] }>; levels: DependencyGraphProps['levels']; links: DependencyGraphProps['links'] }
export interface InstalledTheme { id: string; name: string; builtin: boolean; description: string; version: string; author: string; license: string; modes: string; compat?: { ok: boolean; label: string }; issues?: string[]; swatch?: Record<string, string> }
export interface ThemeManagerProps { installed: InstalledTheme[]; current?: string; onApply?: (id: string) => void }
export declare function validateHltheme(json: string): { errors: string[]; warnings: string[]; theme?: unknown };
/* Catalogue aliases on window.HCIS: TabBar=Tabs, PropertyList=KeyValueList, StatusDot=StateDot, ProgressIndicator=ProgressBar,
   ConfirmationDialog=RiskConfirmation=ConfirmDialog, PermissionMatrix=FileModeEditor=PermissionEditor, DirectoryBrowser=FileBrowser,
   ServiceDetails=ServiceDetail, DockerPermissionsManager=SafePermissionsManager, DriveLayoutDesigner=DriveBayGrid, Toast=ToastRegion. */
