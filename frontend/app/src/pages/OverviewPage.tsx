import { getOverview } from '../api';
import { Overview } from '../Overview';
import { explain, useData } from '../useData';

export function OverviewPage({ onUnauthorized }: { onUnauthorized: () => void }) {
  const { data, error } = useData(getOverview, { intervalMs: 10_000, onUnauthorized });
  return (
    <>
      {error && (
        <p className="banner" role="alert">
          {explain(error)}
        </p>
      )}
      {data ? <Overview data={data} /> : !error && <p className="muted">Reading host data…</p>}
    </>
  );
}
