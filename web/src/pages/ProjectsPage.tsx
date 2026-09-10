import { useState } from "react";
import { Link } from "react-router-dom";
import type { FormEvent } from "react";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { CreateProjectDialog } from "@/components/CreateProjectDialog";
import { useProjects, useRenameProject } from "@/hooks/useProjects";
import type { Project } from "@/hooks/useProjects";
import { useCan } from "@/context/OrgContext";
import { ApiError } from "@/lib/api";

function RenameProjectDialog({ project, onOpenChange }: { project: Project; onOpenChange: (open: boolean) => void }) {
  const renameProject = useRenameProject();
  const [name, setName] = useState(project.name);
  const [formError, setFormError] = useState<string | null>(null);

  const onSubmit = async (e: FormEvent) => {
    e.preventDefault();
    if (!name.trim()) return;
    setFormError(null);
    try {
      await renameProject.mutateAsync({ id: project.id, name: name.trim() });
      onOpenChange(false);
    } catch (err) {
      setFormError(err instanceof ApiError ? err.message : "Could not rename project. Please try again.");
    }
  };

  return (
    <Dialog open onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Rename project</DialogTitle>
        </DialogHeader>
        <form className="flex flex-col gap-4" onSubmit={onSubmit}>
          {formError && (
            <div role="alert" className="rounded-lg bg-destructive/10 px-3 py-2 text-sm text-destructive">
              {formError}
            </div>
          )}
          <div className="flex flex-col gap-1.5">
            <Label htmlFor="rename-name">Name</Label>
            <Input id="rename-name" value={name} onChange={(e) => setName(e.target.value)} autoComplete="off" />
          </div>
          <DialogFooter>
            <Button type="submit" disabled={renameProject.isPending}>
              {renameProject.isPending ? "Saving…" : "Save"}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}

export function ProjectsPage() {
  const { data, isPending, isError } = useProjects();
  const can = useCan();
  const canManage = can("admin");
  const [createOpen, setCreateOpen] = useState(false);
  const [renameTarget, setRenameTarget] = useState<Project | null>(null);

  return (
    <div className="flex flex-col gap-6">
      <div className="flex items-center justify-between">
        <h1 className="text-xl font-semibold">Projects</h1>
        {canManage && <Button onClick={() => setCreateOpen(true)}>New project</Button>}
      </div>

      {isPending && <div className="text-sm text-muted-foreground">Loading…</div>}

      {isError && (
        <div role="alert" className="rounded-lg bg-destructive/10 px-3 py-2 text-sm text-destructive">
          Failed to load projects.
        </div>
      )}

      {!isPending && !isError && data && data.length === 0 && (
        <div className="text-sm text-muted-foreground">No projects yet</div>
      )}

      {!isPending && !isError && data && data.length > 0 && (
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Name</TableHead>
              <TableHead>Slug</TableHead>
              <TableHead>Created</TableHead>
              <TableHead>
                <span className="sr-only">Actions</span>
              </TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {data.map((project) => (
              <TableRow key={project.id}>
                <TableCell>
                  <Link to={`/projects/${project.id}`} className="font-medium text-primary hover:underline">
                    {project.name}
                  </Link>
                </TableCell>
                <TableCell>{project.slug}</TableCell>
                <TableCell>{new Date(project.createdAt).toLocaleDateString()}</TableCell>
                <TableCell>
                  {canManage && (
                    <Button variant="outline" size="sm" onClick={() => setRenameTarget(project)}>
                      Rename
                    </Button>
                  )}
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      )}

      {canManage && <CreateProjectDialog open={createOpen} onOpenChange={setCreateOpen} />}
      {canManage && renameTarget && (
        <RenameProjectDialog
          project={renameTarget}
          onOpenChange={(open) => {
            if (!open) setRenameTarget(null);
          }}
        />
      )}
    </div>
  );
}
