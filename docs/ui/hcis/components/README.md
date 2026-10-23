# HCIS component catalogue

Status: **Production** (in the app), **Production markup** (app markup/CSS, componentized here), **Planned** (in `docs/ui/` 01–10, not built), **Proposed (HCIS)** (new reference design). “Awaiting authorization” marks anything that writes to the host (M2+). Interactive previews live in the HCIS design system artifact (link in `../README.md`).

## Showcase

| Component | Status | Summary |
|---|---|---|
| [ContainerManager](ContainerManager.md) | Proposed (HCIS) · awaiting authorization | Docker: containers grouped by Compose project or flat, images, networks, volumes and engine details. |
| [DriveBayGrid](DriveBayGrid.md) | Proposed (HCIS) · awaiting authorization | Physical Drive Layout Designer: saved layouts per host, irregular grids, HDD/SSD/NVMe bays, reserved slots, custom labels, drag-and-drop or keyboard assignment, list view, zoom, import/export. |
| [HealthDashboard](HealthDashboard.md) | Proposed (HCIS) | System-wide Health & Diagnostics (“Fix common problems”): summary, findings by severity or module, modules checked, scan history. |
| [OverviewDashboard](OverviewDashboard.md) | Proposed (HCIS) | System Overview: live metric tiles, CPU/memory chart with summary, health summary, alerts and background jobs. |
| [ResourceExplorer](ResourceExplorer.md) | Proposed (HCIS) | Cross-module explorer: resource tree, relationship graph and a floating detail panel with related links. |
| [SafePermissionsManager](SafePermissionsManager.md) | Proposed (HCIS) · awaiting authorization | Docker Permissions Manager: choose bind-mounted directories, owner by name/UID, group by name/GID, recursion, separate directory and file modes, then a dry-run with counts and warnings. |
| [ServiceDetail](ServiceDetail.md) | Proposed (HCIS) · awaiting authorization | systemd service page with the Diagnose & Resolve assistant: status, failure category, exit code, restart count, evidence, dependencies, recovery steps, verification. |
| [StorageOverview](StorageOverview.md) | Proposed (HCIS) | Storage landing page: total capacity, pools across technologies, disk health, and relationships to consumers. |
| [ThemeManager](ThemeManager.md) | Proposed (HCIS) | Appearance → Themes: installed themes, safe timed preview, .hltheme import with validation, metadata, requirements and the mandatory tooltip. |

## Production

| Component | Status | Summary |
|---|---|---|
| [Meter](Meter.md) | Production | A 24-segment capacity meter with label, percentage and detail line. |
| [Section](Section.md) | Production | A titled panel that always shows where its data came from, says when data is partial or unavailable, and now shows its live state. |
| [StateDot](StateDot.md) | Production | A small coloured dot that reinforces a state already given in text. |

## Navigation

| Component | Status | Summary |
|---|---|---|
| [AppShell](AppShell.md) | Production | The application frame: glass global header, glass sidebar, workspace, adapting to its own width. |
| [AppearanceMenu](AppearanceMenu.md) | Production | Popover for theme, density, interface font, text size, surfaces (transparency) and motion. |
| [Breadcrumbs](Breadcrumbs.md) | Planned | The path from host to the current resource. |
| [CommandPalette](CommandPalette.md) | Planned | Keyboard-driven search over pages, resources and permitted actions. |
| [GlobalHeader](GlobalHeader.md) | Production (partial) | Identity, active host, health status, workspace, global search, background jobs, notifications, appearance and account. |
| [NavigationRail](NavigationRail.md) | Production | The collapsed, icon-only form of the sidebar (60px). |
| [ResourceHeader](ResourceHeader.md) | Production markup, proposed component | The header of a resource page: kind, name, status, key facts and actions. |
| [ResourceTree](ResourceTree.md) | Proposed (HCIS) | A keyboard-navigable tree of resources (WAI-ARIA tree). |
| [Sidebar](Sidebar.md) | Production (ungrouped) | Grouped, capability-aware primary navigation on glass. |
| [Tabs](Tabs.md) | Planned | Tabs for resource detail views. |
| [WorkspaceSwitcher](WorkspaceSwitcher.md) | Proposed (HCIS) | Switch between saved workspaces (layouts, columns, landing page). |

## Data display

| Component | Status | Summary |
|---|---|---|
| [CapacityBar](CapacityBar.md) | Proposed (HCIS) | A stacked usage bar with legend values and free space. |
| [DataTable](DataTable.md) | Production markup, proposed component | The dense, sortable, selectable table for every resource list. In containers narrower than 560px each row becomes a labelled card. |
| [DependencyGraph](DependencyGraph.md) | Proposed (HCIS) | Columns of related resources with drawn links (host → drive → pool → dataset → mount → container). |
| [DetailPanel](DetailPanel.md) | Proposed (HCIS) | A side panel for the selected resource. `floating` makes it a glass inspector. |
| [FilterBar](FilterBar.md) | Proposed (HCIS) | Search, facet selects, removable filter chips and a live result count above a table. |
| [HealthSummary](HealthSummary.md) | Proposed (HCIS) | Overall health: headline state, counts for Critical, Warning, Notice, Healthy and Unknown, scan control. |
| [KeyValueList](KeyValueList.md) | Production markup, proposed component | Label/value pairs in a responsive grid or two-column rows. |
| [LiveIndicator](LiveIndicator.md) | Proposed (HCIS) | Reactive state of a data source: Live, Refreshing, Stale, Paused, Disconnected. |
| [MetricChart](MetricChart.md) | Planned | A line chart for time series with a hover readout and a table view. |
| [MetricTile](MetricTile.md) | Proposed (HCIS) | A compact KPI: label, value, unit, optional trend and status word. |
| [ResourceCard](ResourceCard.md) | Proposed (HCIS) | A card summarising one resource with a status, a headline metric and optional content. |
| [StatusIndicator](StatusIndicator.md) | Proposed (HCIS) | Icon + word state for dense rows: Healthy, Warning, Critical, Notice, Unknown. |
| [StatusPill](StatusPill.md) | Planned | A tinted label with an icon that names a state. |
| [TimeSeriesChart](TimeSeriesChart.md) | Planned | MetricChart plus an always-visible text summary (now, range, peak) for each series. |

## Input

| Component | Status | Summary |
|---|---|---|
| [Button](Button.md) | Production markup, proposed component | The single action control: primary, secondary, ghost and danger. |
| [Checkbox](Checkbox.md) | Production markup, proposed component | A native checkbox with an indeterminate state. |
| [Combobox](Combobox.md) | Planned | A filterable text field with a listbox (WAI-ARIA combobox). |
| [Dropdown](Dropdown.md) | Planned | A menu button that opens a list of actions. |
| [FileBrowser](FileBrowser.md) | Proposed (HCIS) | A folder tree plus a sortable, selectable file table with owner and mode. |
| [IconButton](IconButton.md) | Planned | A square button showing only an icon, with a required accessible label. |
| [NumericInput](NumericInput.md) | Proposed (HCIS) | A number field with stepper buttons, bounds and unit. |
| [PathPicker](PathPicker.md) | Proposed (HCIS) · awaiting authorization | A mono path field with a Browse dialog. Protected system paths are refused. |
| [PermissionEditor](PermissionEditor.md) | Proposed (HCIS) | An owner/group/others × read/write/execute matrix with the octal and symbolic mode. |
| [RadioGroup](RadioGroup.md) | Planned | Native radios with label and description per option. |
| [SegmentedControl](SegmentedControl.md) | Production markup, proposed component | Radio buttons styled as a compact segmented filter. |
| [Select](Select.md) | Production markup, proposed component | A native select with label and help. |
| [Switch](Switch.md) | Planned | An on/off setting that takes effect as part of a form or preview. |
| [TextField](TextField.md) | Production markup, proposed component | A labelled text input with help text and inline error. |
| [UserGroupPicker](UserGroupPicker.md) | Proposed (HCIS) | Choose an owner or group by name or numeric ID; shows the resolved mapping. |

## Feedback

| Component | Status | Summary |
|---|---|---|
| [Alert](Alert.md) | Proposed (HCIS) | An inline, compact message inside a panel or form. |
| [BackgroundJobPanel](BackgroundJobPanel.md) | Planned · awaiting authorization | A glass panel of background jobs with progress, steps and observed results. |
| [Badge](Badge.md) | Production markup, proposed component | An outlined label for modes and flags, or a numeric count. |
| [Banner](Banner.md) | Production markup, proposed component | A full-width message about the current page or host. |
| [CheckList](CheckList.md) | Production markup, proposed component | A list of findings, each with a level word, a message and an optional action. |
| [ConfirmDialog](ConfirmDialog.md) | Proposed (HCIS) · awaiting authorization | The explicit authorization step (the spec's ConfirmationDialog / RiskConfirmation). |
| [Dialog](Dialog.md) | Planned | A modal built on the native dialog element. |
| [EmptyState](EmptyState.md) | Proposed (HCIS) | What to show when a list is legitimately empty, with the next useful action. |
| [ErrorState](ErrorState.md) | Proposed (HCIS) | A failed load: what failed, that nothing changed, technical detail and Try again. |
| [JobProgress](JobProgress.md) | Planned · awaiting authorization | Progress of one background job, its steps, the observed result and audit reference. |
| [LoadingState](LoadingState.md) | Proposed (HCIS) | Skeleton rows with aria-busy while data loads. |
| [ProgressBar](ProgressBar.md) | Planned | A thin determinate or indeterminate progress bar. |
| [ToastRegion](ToastRegion.md) | Planned | Transient notifications in a polite live region (the spec's Toast). |
| [Tooltip](Tooltip.md) | Planned | An info button that reveals text on hover, focus or press. |

## Administration

| Component | Status | Summary |
|---|---|---|
| [AuditTimeline](AuditTimeline.md) | Proposed (HCIS) | A chronological list of who did what, with outcome and scope. |
| [DiagnosticFinding](DiagnosticFinding.md) | Proposed (HCIS) | One Health finding: severity, confirmed vs suspected, module, first/last seen, evidence, investigation, solution, verification, acknowledge/suppress. |
| [LogViewer](LogViewer.md) | Proposed (HCIS) | A filterable, followable log pane in JetBrains Mono. |
| [OperationPreview](OperationPreview.md) | Proposed (HCIS) · awaiting authorization | The dry-run plan: mode banner, canonical target, counts, every change with diffs, dependency warnings, risk. |
| [ResolutionGuide](ResolutionGuide.md) | Proposed (HCIS) | Ordered Investigate → Fix → Verify steps with copyable commands and documentation links. |
| [ResourceInspector](ResourceInspector.md) | Proposed (HCIS) | A side panel with a resource’s fields and its raw JSON. |
| [RiskBadge](RiskBadge.md) | Proposed (HCIS) | The four HCIS risk classes as a word + icon: Read-only, Reversible, Disruptive, Destructive. |
| [RiskCallout](RiskCallout.md) | Proposed (HCIS) | A boxed statement of risk class and concrete consequences. |
| [TerminalViewer](TerminalViewer.md) | Proposed (HCIS) | A read-only, opaque transcript of commands and output in JetBrains Mono. |

## Domain views

| Component | Status | Summary |
|---|---|---|
| [NetworkInterfaces](NetworkInterfaces.md) | Proposed (HCIS) | Interfaces, bridges, bonds and tunnels with link state, addresses and traffic. |
| [PackageList](PackageList.md) | Proposed (HCIS) | Installed packages and available updates, with security updates called out. |
| [ShareList](ShareList.md) | Proposed (HCIS) | SMB and NFS shares with path, access and status. |
| [StoragePools](StoragePools.md) | Proposed (HCIS) | Pool cards across ZFS, Btrfs, mergerfs, SnapRAID, mdadm and LVM2, filterable by technology. |
| [VMList](VMList.md) | Proposed (HCIS) | Virtual machines with state, resources, CPU sparkline, address and quick actions. |

## Foundations

| Component | Status | Summary |
|---|---|---|
| [Icon](Icon.md) | Planned | The original HCIS icon set: 20px grid, 1.5px stroke, currentColor. |

Spec catalogue names that map to these components: TabBar → Tabs, PropertyList → KeyValueList, SortableColumn → DataTable columns, ProgressIndicator → ProgressBar, Toast → ToastRegion, ConfirmationDialog / RiskConfirmation → ConfirmDialog, PermissionMatrix / FileModeEditor → PermissionEditor, DirectoryBrowser → FileBrowser, ServiceDetails → ServiceDetail, Docker Permissions Manager → SafePermissionsManager, Physical Drive Layout Designer → DriveBayGrid.
