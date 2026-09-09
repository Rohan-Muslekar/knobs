import { useState } from "react";
import { useParams } from "react-router-dom";
import { Button } from "@/components/ui/button";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { CreateEnvironmentDialog } from "@/components/CreateEnvironmentDialog";
import { useEnvironments, useProject } from "@/hooks/useEnvironments";
import { ApiError } from "@/lib/api";

export function ProjectDetailPage() {
  const { projectId } = useParams<{ projectId: string }>();
  const id = projectId ?? "";
  const project = useProject(id);
  const environments = useEnvironments(id);
  const [createOpen, setCreateOpen] = useState(false);

  const isPending = project.isPending || environments.isPending;
  const isNotFound = project.isError && project.error instanceof ApiError && project.error.status === 404;
  const isError = (project.isError && !isNotFound) || environments.isError;

  if (isPending) {
    return <div className="text-sm text-muted-foreground">Loading…</div>;
  }

  if (isNotFound) {
    return <div className="text-sm text-muted-foreground">Project not found</div>;
  }

  if (isError || !project.data) {
    return (
      <div role="alert" className="rounded-lg bg-destructive/10 px-3 py-2 text-sm text-destructive">
        Failed to load project.
      </div>
    );
  }

  const envs = environments.data ?? [];

  return (
    <div className="flex flex-col gap-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-xl font-semibold">{project.data.name}</h1>
          <p className="text-sm text-muted-foreground">{project.data.slug}</p>
        </div>
        <Button onClick={() => setCreateOpen(true)}>New environment</Button>
      </div>

      {envs.length === 0 && <div className="text-sm text-muted-foreground">No environments yet</div>}

      {envs.length > 0 && (
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Name</TableHead>
              <TableHead>Created</TableHead>
              <TableHead>
                <span className="sr-only">Actions</span>
              </TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {envs.map((env) => (
              <TableRow key={env.id}>
                <TableCell className="font-medium">{env.name}</TableCell>
                <TableCell>{new Date(env.createdAt).toLocaleDateString()}</TableCell>
                <TableCell>
                  <Button variant="outline" size="sm" disabled title="Coming in a future release">
                    Configure
                  </Button>
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      )}

      <CreateEnvironmentDialog projectId={id} open={createOpen} onOpenChange={setCreateOpen} />
    </div>
  );
}
