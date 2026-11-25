// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
// SPDX-License-Identifier: GPL-3.0-or-later

import { useCallback, useState } from 'react';
import { ApiError, getStorage, probeStorage, type DriveLayout } from '../api';
import { SLOT_MATCH_TEXT, hardwareLink, locationBadges, slotText } from '../bayModel';
import { DataTable } from '../DataTable';
import { formatBytes } from '../format';
import { Section } from '../ui';
import { DriveBays } from './DriveBays';
import { PartitionPlan } from './PartitionPlan';
import { explain, useData } from '../useData';
import {
  DRIVE_LABEL_KEY,
  driveLabel,
  pvDeviceLinkText,
  savedDriveLabel,
  type DriveLabelPreference,
  type StorageInventory,
  type StorageNode,
  type StorageSMART,
} from '../storageModel';

export function Storage({
  csrf,
  scopes = [],
  onUnauthorized,
}: {
  csrf: string;
  scopes?: string[];
  onUnauthorized: () => void;
}) {
  const fetchStorage = useCallback(() => getStorage(csrf), [csrf]);
  const {
    data: apiData,
    error,
    live,
    reload,
  } = useData(fetchStorage, { onUnauthorized, intervalMs: 60_000 });
  const [preference, setPreference] = useState<DriveLabelPreference>(savedDriveLabel);
  const [query, setQuery] = useState('');
  const [selected, setSelected] = useState<{ id: string; at: string } | null>(null);
  const [preferenceNote, setPreferenceNote] = useState('');
  const [probed, setProbed] = useState<StorageInventory | null>(null);
  const [probing, setProbing] = useState(false);
  const [probeMessage, setProbeMessage] = useState('');
  const canProbe = scopes.includes('storage.probe');
  // Stage B: the manual layout the Drives & bays panel already read, used for
  // location badges. No extra request.
  const [layout, setLayout] = useState<DriveLayout | undefined>(undefined);

  async function probe() {
    setProbing(true);
    setProbeMessage('');
    try {
      const result = await probeStorage(csrf);
      setProbed(result);
      setSelected(null);
      setProbeMessage(
        result.graph.status === 'partial'
          ? 'Probe complete; some devices were skipped or failed and are shown as partial.'
          : 'Probe complete.',
      );
    } catch (err) {
      if (err instanceof ApiError && err.status === 401) onUnauthorized();
      setProbeMessage(err instanceof ApiError ? err.message : 'Probe failed.');
    } finally {
      setProbing(false);
    }
  }

  const data = probed ?? apiData;

  if (!data) {
    return (
      <div className="page">
        <h1 className="page-title">Storage</h1>
        <p role={error ? 'alert' : 'status'}>{error ? explain(error) : 'Reading storage metadata…'}</p>
      </div>
    );
  }
  const { graph } = data;
  const nodes = new Map(graph.nodes.map((node) => [node.id, node]));
  const claims = new Map<string, string[]>();
  for (const claim of graph.claims)
    claims.set(claim.node, [...(claims.get(claim.node) ?? []), claim.technology]);
  const needle = query.trim().toLocaleLowerCase();
  const rows = graph.nodes.filter((node) =>
    [node.name, node.kind, node.persistent_id ?? '', ...(node.identifiers ?? []).map((id) => id.value)].some(
      (value) => value.toLocaleLowerCase().includes(needle),
    ),
  );
  // Node keys are snapshot-local. Never carry selection onto a different
  // observation merely because a kernel device number was reused.
  const selectedID = selected?.at === data.collected_at ? selected.id : undefined;
  const current = selectedID ? nodes.get(selectedID) : undefined;
  const edges = graph.edges.filter((edge) => edge.from === selectedID || edge.to === selectedID);
  const meta = {
    status: graph.status,
    sources: [...new Set(graph.nodes.flatMap((node) => node.evidence.map((evidence) => evidence.source)))],
    notes: graph.notes,
  };
  const hardware = graph.hardware;
  const badges = locationBadges(layout?.chassis ?? [], graph.nodes);
  const controllers = new Map((hardware?.controllers ?? []).map((c) => [c.id, c]));
  const controllerText = (id?: string) => {
    const c = id ? controllers.get(id) : undefined;
    if (!c) return undefined;
    return [c.model || c.driver || c.kind, c.pci_address && `PCI ${c.pci_address}`]
      .filter(Boolean)
      .join(', ');
  };
  const nodeLabel = (id: string) => {
    const node = nodes.get(id);
    return node ? driveLabel(node, preference).value : 'Unobserved resource';
  };

  function changePreference(value: DriveLabelPreference) {
    setPreference(value);
    try {
      localStorage.setItem(DRIVE_LABEL_KEY, value);
      setPreferenceNote('Drive label preference saved in this browser.');
    } catch {
      setPreferenceNote('Preference applies to this page; browser storage is unavailable.');
    }
  }

  return (
    <div className="page">
      <header className="page-head">
        <h1 className="page-title">Storage</h1>
        <p className="page-sub">Read-only observations. No storage configuration is changed here.</p>
      </header>
      {error && (
        <p className="banner" role="alert">
          {explain(error)} Previously collected data may be stale.
        </p>
      )}
      <p className="banner">
        Missing claims do not mean a drive is unused. RAID/cache health and physical locations are unknown
        unless explicitly observed. Layered sizes must not be added together.
      </p>
      {graph.notes.length > 0 && (
        <ul aria-label="Inventory limitations">
          {graph.notes.map((note) => (
            <li key={note}>{note}</li>
          ))}
        </ul>
      )}
      <div className="toolbar">
        <button type="button" className="ghost" onClick={reload}>
          Refresh inventory
        </button>
        {canProbe && (
          <button type="button" disabled={probing} onClick={() => void probe()}>
            {probing ? 'Probing…' : 'Probe storage'}
          </button>
        )}
        <div className="inline-field">
          <label htmlFor="storage-drive-label">Identify drives by</label>
          <select
            id="storage-drive-label"
            value={preference}
            onChange={(event) => changePreference(event.target.value === 'wwn' ? 'wwn' : 'serial-id')}
          >
            <option value="serial-id">Serial / ID</option>
            <option value="wwn">WWN</option>
          </select>
        </div>
        <label className="inline-field">
          Find a resource
          <input
            type="search"
            value={query}
            maxLength={256}
            onChange={(event) => setQuery(event.target.value)}
          />
        </label>
      </div>
      <p className="muted" role="status">
        {probeMessage || preferenceNote}
      </p>
      {canProbe && (
        <p className="muted">
          Probe runs the read-only tools lsblk, smartctl and pvs/lvs. A disk in standby is not woken; its
          SMART health shows as standby. It runs only when you click it, never automatically.
        </p>
      )}
      <Section title="Storage resources" id="storage-resources" live={live} meta={meta}>
        <p className="muted">
          Collected {new Date(data.collected_at).toLocaleString()}. Display preference never changes
          persistent identity or bay assignments.
        </p>
        <p className="muted">
          {data.snapshot_persisted
            ? `Saved observation ${data.snapshot_id ?? 'available'} is preserved locally.`
            : 'This observation is not saved because the state database is not enabled.'}
        </p>
        <DataTable
          caption="Observed storage resources"
          rows={rows}
          rowKey={(node) => node.id}
          columns={[
            {
              id: 'identity',
              label: 'Identifier',
              rowHeader: true,
              sortValue: (node) => driveLabel(node, preference).value,
              render: (node) => {
                const label = driveLabel(node, preference);
                return (
                  <>
                    <button
                      type="button"
                      className="ghost mono"
                      onClick={() => setSelected({ id: node.id, at: data.collected_at })}
                      aria-label={`Inspect ${label.value}`}
                    >
                      {label.value || 'Unnamed resource'}
                    </button>
                    <div className="muted">{label.kind}</div>
                    {label.warning && <p>{label.warning}</p>}
                  </>
                );
              },
            },
            {
              id: 'kind',
              label: 'Resource type',
              sortValue: (node) => node.kind,
              render: (node) => node.kind,
            },
            {
              id: 'location',
              label: 'Bay (manual)',
              sortValue: (node) => {
                const badge = badges.get(node.id);
                return badge ? `${badge.chassis} ${badge.bay}` : '';
              },
              render: (node) => {
                if (node.kind !== 'block') return '—';
                const badge = badges.get(node.id);
                if (!badge) return <span className="muted">None</span>;
                return (
                  <>
                    <span className="tag location-badge">
                      {badge.chassis} · {badge.bay}
                    </span>
                    <div className="muted">Manual, unverified</div>
                  </>
                );
              },
            },
            {
              id: 'size',
              label: 'Observed size',
              sortValue: (node) => node.size_bytes ?? -1,
              render: (node) =>
                node.size_bytes === undefined ? 'Not reported' : formatBytes(node.size_bytes),
            },
            {
              id: 'use',
              label: 'Observed use',
              render: (node) => (claims.get(node.id) ?? []).join(', ') || 'Not established',
            },
            { id: 'identity-status', label: 'Identity status', render: (node) => node.identity_status },
            {
              id: 'smart',
              label: 'SMART health',
              render: (node) => (node.smart_health ? smartHealthText[node.smart_health] : 'Not read'),
            },
          ]}
        />
      </Section>
      <Section
        title="LVM2 RAID and SSD caching"
        id="lvm-observations"
        meta={{
          status: graph.lvm ? 'partial' : 'unavailable',
          sources: graph.lvm ? [graph.lvm.source] : [],
          notes: graph.lvm
            ? [
                graph.lvm.mode === 'metadata-only'
                  ? 'Read in metadata-only mode: RAID synchronization, cache usage and health were not observed and show as unknown.'
                  : 'Reported runtime values do not by themselves establish physical health or complete redundancy.',
              ]
            : [
                'Native LVM reporting is unavailable in this metadata-only collector. No RAID or cache health has been assumed.',
              ],
        }}
      >
        {graph.lvm && (
          <DataTable
            caption="Observed LVM logical volumes and segments"
            rows={graph.lvm.volumes}
            rowKey={(volume) => volume.uuid}
            columns={[
              {
                id: 'name',
                label: 'Logical volume',
                rowHeader: true,
                render: (volume) => (
                  <span className="mono">
                    {volume.vg_name}/{volume.name}
                  </span>
                ),
              },
              {
                id: 'layout',
                label: 'Reported layout',
                render: (volume) => volume.segments.map((segment) => segment.type).join(', '),
              },
              { id: 'health', label: 'Reported health', render: (volume) => volume.health || 'Unknown' },
              {
                id: 'sync',
                label: 'Synchronization',
                render: (volume) =>
                  [
                    volume.sync_action,
                    volume.sync_percent === undefined ? undefined : `${volume.sync_percent}%`,
                  ]
                    .filter(Boolean)
                    .join(' ') || 'Not reported',
              },
              {
                id: 'cache',
                label: 'Cache mode / policy',
                render: (volume) =>
                  volume.segments
                    .map((segment) => [segment.cache_mode, segment.cache_policy].filter(Boolean).join(' / '))
                    .filter(Boolean)
                    .join(', ') || 'Not reported',
              },
              {
                id: 'kernel',
                label: 'Kernel device',
                render: (volume) =>
                  ({
                    joined: 'Matched by UUID',
                    'no-kernel-node': 'None observed (inactive, internal or layered)',
                    ambiguous: 'Ambiguous — not associated',
                    'not-checked': 'Not checked',
                  })[volume.kernel_join ?? 'not-checked'],
              },
              {
                id: 'dirty',
                label: 'Dirty cache blocks',
                render: (volume) =>
                  volume.cache_dirty_blocks === undefined
                    ? 'Not reported'
                    : String(volume.cache_dirty_blocks),
              },
            ]}
            emptyState="No logical volumes reported."
          />
        )}
        {graph.lvm && (
          <DataTable
            caption="Observed LVM physical volumes"
            rows={graph.lvm.physical_volumes}
            rowKey={(pv) => pv.uuid}
            columns={[
              {
                id: 'pv',
                label: 'Physical volume',
                rowHeader: true,
                render: (pv) => <span className="mono">{pv.name || pv.uuid}</span>,
              },
              { id: 'vg', label: 'Volume group', render: (pv) => pv.vg_name || 'Not in a VG' },
              {
                id: 'device',
                label: 'Kernel device number',
                render: (pv) =>
                  pv.ambiguous
                    ? 'Ambiguous — PV UUID reported on several devices'
                    : (pv.device_number ?? 'Not reported'),
              },
              { id: 'link', label: 'Disk link', render: (pv) => pvDeviceLinkText(pv) },
              {
                id: 'allocated',
                label: 'Allocated to',
                render: (pv) =>
                  [
                    ...new Set(
                      pv.segments
                        .map(
                          (segment) =>
                            graph.lvm?.volumes.find((volume) => volume.uuid === segment.lv_uuid)?.name,
                        )
                        .filter(Boolean),
                    ),
                  ].join(', ') || 'No allocated extents reported',
              },
              {
                id: 'missing',
                label: 'LVM reports missing',
                render: (pv) =>
                  pv.missing_state === 'not-reported'
                    ? 'Not reported'
                    : pv.missing_state === 'missing-from-attr'
                      ? 'Yes (from pv_attr)'
                      : pv.missing
                        ? 'Yes'
                        : 'No',
              },
            ]}
            emptyState="No physical volume report available; which disks supply each volume group is not shown."
          />
        )}
      </Section>
      <Section
        title="Controllers and enclosures"
        id="storage-hardware"
        meta={{
          status: hardware?.status === 'observed' ? 'partial' : 'unavailable',
          sources: hardware ? [hardware.source] : [],
          notes: hardware?.notes ?? ['This observation carries no controller or enclosure data.'],
        }}
      >
        <p className="banner">
          Read-only hardware evidence from the kernel. An enclosure slot is numbered by the enclosure
          firmware; it is not a verified front-panel bay and never changes a bay assignment. Nothing here
          blinks LEDs or writes to the enclosure.
        </p>
        {hardware && (
          <DataTable
            caption="Observed storage controllers"
            rows={hardware.controllers}
            rowKey={(c) => c.id}
            columns={[
              {
                id: 'controller',
                label: 'Controller',
                rowHeader: true,
                render: (c) => <span className="mono">{c.id}</span>,
              },
              { id: 'kind', label: 'Type', render: (c) => c.kind },
              { id: 'driver', label: 'Driver', render: (c) => c.driver || 'Not reported' },
              { id: 'model', label: 'Model', render: (c) => c.model || 'Not reported' },
              { id: 'firmware', label: 'Firmware', render: (c) => c.firmware || 'Not reported' },
              {
                id: 'sas',
                label: 'SAS address',
                render: (c) => (c.sas_address ? <span className="mono">{c.sas_address}</span> : '—'),
              },
              {
                id: 'drives',
                label: 'Drives linked',
                render: (c) => String(hardware.links.filter((l) => l.controller_id === c.id).length),
              },
            ]}
            emptyState="No storage controller reported."
          />
        )}
        {hardware?.enclosures.map((enc) => (
          <DataTable
            key={`${enc.name}:${enc.id ?? ''}`}
            caption={`Enclosure ${enc.id ?? enc.name}${[enc.vendor, enc.model].some(Boolean) ? ` (${[enc.vendor, enc.model].filter(Boolean).join(' ')})` : ''}${enc.identity_status === 'observed' ? '' : enc.identity_status === 'ambiguous' ? ' — reported more than once, not matched' : ' — no identifier, not matched'}`}
            rows={enc.slots}
            rowKey={(slot) => slot.element}
            columns={[
              {
                id: 'slot',
                label: 'Slot',
                rowHeader: true,
                render: (slot) => (slot.slot === undefined ? `Element ${slot.element}` : `Slot ${slot.slot}`),
              },
              {
                id: 'element',
                label: 'Element',
                render: (slot) => <span className="mono">{slot.element}</span>,
              },
              { id: 'status', label: 'Reported status', render: (slot) => slot.status || 'Not reported' },
              {
                id: 'drive',
                label: 'Drive',
                render: (slot) => {
                  const node = slot.node_id ? nodes.get(slot.node_id) : undefined;
                  if (slot.match === 'matched' && node)
                    return <span className="mono">{driveLabel(node, preference).value}</span>;
                  return SLOT_MATCH_TEXT[slot.match];
                },
              },
              {
                id: 'bay',
                label: 'Bay (manual)',
                render: (slot) => {
                  const badge =
                    slot.match === 'matched' && slot.node_id ? badges.get(slot.node_id) : undefined;
                  return badge ? `${badge.chassis} · ${badge.bay}` : <span className="muted">None</span>;
                },
              },
            ]}
            emptyState="The enclosure reported no elements."
          />
        ))}
      </Section>
      <DriveBays
        csrf={csrf}
        nodes={graph.nodes}
        canManage={scopes.includes('layout.manage')}
        preference={preference}
        onUnauthorized={onUnauthorized}
        hardware={hardware}
        onLayout={setLayout}
      />
      {current && (
        <Section
          title="Selected resource"
          id="selected-storage-resource"
          meta={{ status: 'ok', sources: current.evidence.map((evidence) => evidence.source) }}
        >
          <h3 className="subhead mono">{driveLabel(current, preference).value}</h3>
          {(current.kind === 'block' || current.kind === 'disk') && (
            <PartitionPlan
              key={current.id}
              disk={current}
              nodes={nodes}
              edges={edges}
              csrf={csrf}
              onUnauthorized={onUnauthorized}
            />
          )}
          <dl className="facts">
            <div>
              <dt>Kernel/display name</dt>
              <dd className="mono">{current.name}</dd>
            </div>
            <div>
              <dt>Persistent identity</dt>
              <dd className="mono">{current.persistent_id ?? 'Not established'}</dd>
            </div>
            <div>
              <dt>Mount view</dt>
              <dd>
                {current.mount_view === 'service'
                  ? 'API service namespace — not verified host mounts'
                  : current.mount_view === 'host'
                    ? 'Verified host namespace'
                    : 'Not applicable'}
              </dd>
            </div>
            <div>
              <dt>Mount root</dt>
              <dd className="mono">{current.mount_root ?? 'Not applicable'}</dd>
            </div>
            {current.md && (
              <div>
                <dt>md runtime state (sysfs)</dt>
                <dd>
                  {[
                    current.md.level,
                    current.md.array_state && `array ${current.md.array_state}`,
                    current.md.degraded !== undefined && `degraded ${current.md.degraded}`,
                    current.md.raid_disks !== undefined && `raid disks ${current.md.raid_disks}`,
                    current.md.sync_action && `sync ${current.md.sync_action}`,
                    current.md.sync_completed && `progress ${current.md.sync_completed}`,
                    current.md.mismatch_count !== undefined && `mismatches ${current.md.mismatch_count}`,
                  ]
                    .filter(Boolean)
                    .join(' · ')}
                  {current.md.members.length > 0 && (
                    <ul aria-label="md members">
                      {current.md.members.map((member, index) => (
                        <li key={`${member.device_number ?? 'unknown'}:${index}`}>
                          <span className="mono">{member.device_number ?? 'unidentified member'}</span> — slot{' '}
                          {member.slot ?? 'not reported'}, state {member.state ?? 'not reported'}
                        </li>
                      ))}
                    </ul>
                  )}
                </dd>
              </div>
            )}
            {current.smart && (
              <div>
                <dt>SMART (smartctl)</dt>
                <dd>
                  <SmartFacts smart={current.smart} health={current.smart_health} />
                </dd>
              </div>
            )}
            <div>
              <dt>Bay (manual, unverified)</dt>
              <dd>
                {badges.get(current.id)
                  ? `${badges.get(current.id)?.chassis} · ${badges.get(current.id)?.bay}`
                  : 'None — no manual bay assignment is present for this drive'}
              </dd>
            </div>
            <div>
              <dt>Controller (hardware evidence)</dt>
              <dd>
                {[
                  controllerText(hardwareLink(hardware, current.id)?.controller_id),
                  hardwareLink(hardware, current.id)?.port,
                ]
                  .filter(Boolean)
                  .join(', ') || 'Not reported'}
              </dd>
            </div>
            <div>
              <dt>Enclosure slot (hardware evidence, not a verified bay)</dt>
              <dd>{slotText(hardwareLink(hardware, current.id) ?? {}) ?? 'Not reported'}</dd>
            </div>
          </dl>
          {(current.notes ?? []).map((note) => (
            <p key={note}>{note}</p>
          ))}
          <h3 className="subhead">Alternative identifiers</h3>
          <ul>
            {(current.identifiers ?? []).map((id) => (
              <li key={`${id.kind}:${id.value}`}>
                <span className="mono">
                  {id.kind}: {id.value}
                </span>{' '}
                — {id.ambiguous ? 'Duplicated, ambiguous' : 'Observed'} ({id.source}
                {id.scope === 'controller' ? '; shared by every namespace of this controller' : ''})
              </li>
            ))}
          </ul>
          {!current.identifiers?.length && <p>No additional identifiers observed.</p>}
          <DataTable
            caption="Relationships of selected resource"
            rows={edges}
            rowKey={(edge) => `${edge.from}:${edge.kind}:${edge.to}`}
            columns={[
              { id: 'from', label: 'From', render: (edge) => nodeLabel(edge.from) },
              { id: 'relation', label: 'Relationship', render: (edge) => edge.kind },
              { id: 'to', label: 'To', render: (edge) => nodeLabel(edge.to) },
              {
                id: 'source',
                label: 'Evidence',
                render: (edge) => `${edge.evidence.source} (${edge.evidence.confidence})`,
              },
            ]}
            emptyState="No relationships observed."
          />
          <button type="button" className="ghost" onClick={() => setSelected(null)}>
            Close resource details
          </button>
        </Section>
      )}
    </div>
  );
}

const smartHealthText: Record<NonNullable<StorageNode['smart_health']>, string> = {
  ok: 'Passed',
  warning: 'Warning',
  failing: 'Failing',
  standby: 'Standby (not woken)',
  unknown: 'Unknown',
};

/** SMART facts for one disk. Values are shown only when smartctl reported them. */
function SmartFacts({ smart, health }: { smart: StorageSMART; health?: StorageNode['smart_health'] }) {
  const facts: [string, string | number | undefined][] = [
    ['Health', health ? smartHealthText[health] : undefined],
    ['Protocol', smart.protocol],
    ['Model', smart.model],
    ['Firmware', smart.firmware],
    ['Temperature', smart.temperature_c === undefined ? undefined : `${smart.temperature_c} °C`],
    ['Power-on hours', smart.power_on_hours],
    ['Reallocated sectors', smart.reallocated_sectors],
    ['Pending sectors', smart.pending_sectors],
    ['Offline uncorrectable', smart.offline_uncorrectable],
    ['Interface CRC errors', smart.udma_crc_errors],
    ['NVMe critical warning', smart.nvme_critical_warning],
    [
      'NVMe percentage used',
      smart.nvme_percentage_used === undefined ? undefined : `${smart.nvme_percentage_used} %`,
    ],
    ['NVMe media errors', smart.nvme_media_errors],
    [
      'NVMe available spare',
      smart.nvme_available_spare === undefined ? undefined : `${smart.nvme_available_spare} %`,
    ],
  ];
  return (
    <>
      <ul aria-label="SMART values">
        {facts
          .filter(([, value]) => value !== undefined)
          .map(([label, value]) => (
            <li key={label}>
              {label}: {value}
            </li>
          ))}
      </ul>
      {(smart.messages ?? []).map((message) => (
        <p key={message} className="muted">
          smartctl: {message}
        </p>
      ))}
    </>
  );
}
