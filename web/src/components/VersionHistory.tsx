import { useState } from "react";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { useRollback, useVersions } from "@/hooks/useVersions";
import { useCan } from "@/context/OrgContext";
import { ApiError } from "@/lib/api";

export function VersionHistory({ envId }: { envId: string }) {
  const versions = useVersions(envId);
  const rollback = useRollback(envId);
  const can = useCan();
  const canRollback = can("editor");

  const [pendingVersion, setPendingVersion] = useState<number | null>(null);
  const [error, setError] = useState<string | null>(null);

  const closeDialog = () => {
    setPendingVersion(null);
    setError(null);
  };

  const onConfirmRollback = async () => {
    if (pendingVersion === null) return;
    setError(null);
    try {
      await rollback.mutateAsync({ version: pendingVersion });
      toast.success(`Rolled back to version ${pendingVersion}`);
      setPendingVersion(null);
    } catch (err) {
      if (err instanceof ApiError) {
        setError(err.message);
      } else {
        setError("Something went wrong. Please try again.");
      }
    }
  };

  if (versions.isPending) {
    return <div className="text-sm text-muted-foreground">Loading version history…</div>;
  }

  if (versions.isError || !versions.data) {
    return (
      <div role="alert" className="rounded-lg bg-destructive/10 px-3 py-2 text-sm text-destructive">
        Failed to load version history.
      </div>
    );
  }

  return (
    <div className="flex flex-col gap-3">
      <h2 className="text-sm font-medium">Version history</h2>

      {versions.data.length === 0 ? (
        <div className="text-sm text-muted-foreground">No version history yet.</div>
      ) : (
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Version</TableHead>
              <TableHead>Created</TableHead>
              <TableHead>Created by</TableHead>
              <TableHead>Status</TableHead>
              <TableHead />
            </TableRow>
          </TableHeader>
          <TableBody>
            {versions.data.map((v) => (
              <TableRow key={v.id}>
                <TableCell>{v.version}</TableCell>
                <TableCell>{new Date(v.createdAt).toLocaleDateString()}</TableCell>
                <TableCell>{v.createdBy}</TableCell>
                <TableCell>
                  {v.isCurrent && (
                    <span className="rounded-full bg-primary/10 px-2 py-0.5 text-xs font-medium text-primary">
                      Current
                    </span>
                  )}
                </TableCell>
                <TableCell>
                  {!v.isCurrent && canRollback && (
                    <Button variant="outline" size="sm" onClick={() => setPendingVersion(v.version)}>
                      Roll back
                    </Button>
                  )}
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      )}

      <Dialog open={pendingVersion !== null} onOpenChange={(open) => !open && closeDialog()}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Roll back to version {pendingVersion}?</DialogTitle>
          </DialogHeader>

          {error && (
            <div role="alert" className="rounded-lg bg-destructive/10 px-3 py-2 text-sm text-destructive">
              {error}
            </div>
          )}

          <DialogFooter>
            <Button variant="outline" onClick={closeDialog}>
              Cancel
            </Button>
            <Button onClick={onConfirmRollback} disabled={rollback.isPending}>
              {rollback.isPending ? "Rolling back…" : "Confirm"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}
