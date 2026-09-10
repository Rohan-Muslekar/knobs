import { useState } from "react";
import type { FormEvent } from "react";
import { useParams } from "react-router-dom";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { useAddMember, useMembers, useRemoveMember, useUpdateMemberRole } from "@/hooks/useOrganizations";
import type { Member } from "@/hooks/useOrganizations";
import { useOrg } from "@/context/OrgContext";
import type { Role } from "@/lib/roles";
import { ApiError } from "@/lib/api";

const ROLES: Role[] = ["viewer", "editor", "admin", "owner"];

function AddMemberDialog({
  orgId,
  open,
  onOpenChange,
  onProvisioned,
}: {
  orgId: string;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onProvisioned: (info: { email: string; temporaryPassword: string }) => void;
}) {
  const addMember = useAddMember(orgId);
  const [email, setEmail] = useState("");
  const [role, setRole] = useState<Role>("viewer");
  const [formError, setFormError] = useState<string | null>(null);

  const handleOpenChange = (next: boolean) => {
    if (!next) {
      setEmail("");
      setRole("viewer");
      setFormError(null);
    }
    onOpenChange(next);
  };

  const onSubmit = async (e: FormEvent) => {
    e.preventDefault();
    if (!email.trim()) return;
    setFormError(null);
    try {
      const result = await addMember.mutateAsync({ email: email.trim(), role });
      handleOpenChange(false);
      if (result.temporaryPassword) {
        onProvisioned({ email: result.email, temporaryPassword: result.temporaryPassword });
      } else {
        toast.success("Member added");
      }
    } catch (err) {
      setFormError(err instanceof ApiError ? err.message : "Something went wrong. Please try again.");
    }
  };

  return (
    <Dialog open={open} onOpenChange={handleOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Add member</DialogTitle>
        </DialogHeader>
        <form className="flex flex-col gap-4" onSubmit={onSubmit}>
          {formError && (
            <div role="alert" className="rounded-lg bg-destructive/10 px-3 py-2 text-sm text-destructive">
              {formError}
            </div>
          )}
          <div className="flex flex-col gap-1.5">
            <Label htmlFor="member-email">Email</Label>
            <Input
              id="member-email"
              type="email"
              autoComplete="off"
              value={email}
              onChange={(e) => setEmail(e.target.value)}
            />
          </div>
          <div className="flex flex-col gap-1.5">
            <Label htmlFor="member-role">Role</Label>
            <Select value={role} onValueChange={(next) => setRole(next as Role)}>
              <SelectTrigger id="member-role" aria-label="Role">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {ROLES.map((r) => (
                  <SelectItem key={r} value={r}>
                    {r}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
          <DialogFooter>
            <Button type="submit" disabled={addMember.isPending}>
              {addMember.isPending ? "Adding…" : "Add"}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}

function TempPasswordDialog({
  info,
  onOpenChange,
}: {
  info: { email: string; temporaryPassword: string };
  onOpenChange: (open: boolean) => void;
}) {
  const [copied, setCopied] = useState(false);

  const copy = async () => {
    try {
      await navigator.clipboard.writeText(info.temporaryPassword);
      setCopied(true);
    } catch {
      // Clipboard access can be blocked (permissions, insecure context); the
      // password is still visible on screen to copy by hand.
    }
  };

  return (
    <Dialog open onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Temporary password for {info.email}</DialogTitle>
        </DialogHeader>
        <div className="flex flex-col gap-3">
          <p className="text-sm text-muted-foreground">
            This password is shown once and can&rsquo;t be retrieved again. Copy it now and share it with{" "}
            {info.email} out of band.
          </p>
          <div className="flex items-center gap-2">
            <Input readOnly value={info.temporaryPassword} className="font-mono" />
            <Button type="button" variant="outline" onClick={copy}>
              {copied ? "Copied" : "Copy"}
            </Button>
          </div>
        </div>
        <DialogFooter>
          <Button onClick={() => onOpenChange(false)}>Done</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

export function MembersPage() {
  const { orgId } = useParams<{ orgId: string }>();
  const id = orgId ?? "";
  const { organizations } = useOrg();
  const org = organizations.find((o) => o.id === id);
  const isOwner = org?.role === "owner";

  const members = useMembers(id);
  const updateRole = useUpdateMemberRole(id);
  const removeMember = useRemoveMember(id);

  const [addOpen, setAddOpen] = useState(false);
  const [provisioned, setProvisioned] = useState<{ email: string; temporaryPassword: string } | null>(null);
  const [removeTarget, setRemoveTarget] = useState<Member | null>(null);
  const [rowError, setRowError] = useState<string | null>(null);

  const onChangeRole = async (member: Member, role: Role) => {
    setRowError(null);
    try {
      await updateRole.mutateAsync({ userId: member.userId, role });
    } catch (err) {
      setRowError(err instanceof ApiError ? err.message : "Something went wrong. Please try again.");
    }
  };

  const onConfirmRemove = async () => {
    if (!removeTarget) return;
    setRowError(null);
    try {
      await removeMember.mutateAsync(removeTarget.userId);
      setRemoveTarget(null);
    } catch (err) {
      setRowError(err instanceof ApiError ? err.message : "Something went wrong. Please try again.");
    }
  };

  if (members.isPending) {
    return <div className="text-sm text-muted-foreground">Loading…</div>;
  }

  if (members.isError || !members.data) {
    return (
      <div role="alert" className="rounded-lg bg-destructive/10 px-3 py-2 text-sm text-destructive">
        Failed to load members.
      </div>
    );
  }

  return (
    <div className="flex flex-col gap-6">
      <div className="flex items-center justify-between">
        <h1 className="text-xl font-semibold">Members</h1>
        <Button onClick={() => setAddOpen(true)}>Add member</Button>
      </div>

      {rowError && (
        <div role="alert" className="rounded-lg bg-destructive/10 px-3 py-2 text-sm text-destructive">
          {rowError}
        </div>
      )}

      {members.data.length === 0 ? (
        <div className="text-sm text-muted-foreground">No members yet</div>
      ) : (
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Email</TableHead>
              <TableHead>Role</TableHead>
              <TableHead>
                <span className="sr-only">Actions</span>
              </TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {members.data.map((member) => (
              <TableRow key={member.userId}>
                <TableCell>{member.email}</TableCell>
                <TableCell>
                  {isOwner ? (
                    <Select value={member.role} onValueChange={(next) => onChangeRole(member, next as Role)}>
                      <SelectTrigger size="sm" aria-label={`Role for ${member.email}`}>
                        <SelectValue />
                      </SelectTrigger>
                      <SelectContent>
                        {ROLES.map((r) => (
                          <SelectItem key={r} value={r}>
                            {r}
                          </SelectItem>
                        ))}
                      </SelectContent>
                    </Select>
                  ) : (
                    <span className="capitalize">{member.role}</span>
                  )}
                </TableCell>
                <TableCell>
                  {isOwner && (
                    <Button variant="outline" size="sm" onClick={() => setRemoveTarget(member)}>
                      Remove
                    </Button>
                  )}
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      )}

      <AddMemberDialog orgId={id} open={addOpen} onOpenChange={setAddOpen} onProvisioned={setProvisioned} />

      {provisioned && (
        <TempPasswordDialog info={provisioned} onOpenChange={(open) => !open && setProvisioned(null)} />
      )}

      <Dialog open={removeTarget !== null} onOpenChange={(open) => !open && setRemoveTarget(null)}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Remove {removeTarget?.email}?</DialogTitle>
          </DialogHeader>
          <DialogFooter>
            <Button variant="outline" onClick={() => setRemoveTarget(null)}>
              Cancel
            </Button>
            <Button onClick={onConfirmRemove} disabled={removeMember.isPending}>
              {removeMember.isPending ? "Removing…" : "Confirm"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}
