import { Link, useParams } from "react-router-dom";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { useAudit } from "@/hooks/useAudit";
import { useProject } from "@/hooks/useEnvironments";
import { ApiError } from "@/lib/api";

export function AuditPage() {
  const { projectId } = useParams<{ projectId: string }>();
  const id = projectId ?? "";
  const project = useProject(id);
  const audit = useAudit(id);

  const isPending = project.isPending || audit.isPending;
  const isNotFound = project.isError && project.error instanceof ApiError && project.error.status === 404;
  const isProjectError = project.isError && !isNotFound;
  const isError = isProjectError || audit.isError;

  if (isPending) {
    return <div className="text-sm text-muted-foreground">Loading…</div>;
  }

  if (isNotFound) {
    return <div className="text-sm text-muted-foreground">Project not found</div>;
  }

  if (isError || !project.data || !audit.data) {
    return (
      <div role="alert" className="rounded-lg bg-destructive/10 px-3 py-2 text-sm text-destructive">
        {isProjectError || !project.data ? "Failed to load project." : "Failed to load audit log."}
      </div>
    );
  }

  const entries = [...audit.data].sort((a, b) => new Date(b.at).getTime() - new Date(a.at).getTime());

  return (
    <div className="flex flex-col gap-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-xl font-semibold">Audit log</h1>
          <p className="text-sm text-muted-foreground">{project.data.name}</p>
        </div>
        <Link to={`/projects/${id}`} className="text-sm text-muted-foreground hover:text-foreground">
          Back to project
        </Link>
      </div>

      {entries.length === 0 && <div className="text-sm text-muted-foreground">No activity yet</div>}

      {entries.length > 0 && (
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Time</TableHead>
              <TableHead>Actor</TableHead>
              <TableHead>Action</TableHead>
              <TableHead>Target</TableHead>
              <TableHead>Diff</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {entries.map((entry) => (
              <TableRow key={entry.id}>
                <TableCell>{new Date(entry.at).toLocaleString()}</TableCell>
                <TableCell>{entry.actor}</TableCell>
                <TableCell>{entry.action}</TableCell>
                <TableCell>{entry.target}</TableCell>
                <TableCell>
                  <pre className="max-w-xs overflow-x-auto whitespace-pre-wrap text-xs text-muted-foreground">
                    <code>{JSON.stringify(entry.diff)}</code>
                  </pre>
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      )}
    </div>
  );
}
