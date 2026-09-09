import { useState } from "react";
import { useForm } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { z } from "zod";
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
import { useCreateEnvironment } from "@/hooks/useEnvironments";
import { ApiError } from "@/lib/api";

const createEnvironmentSchema = z.object({
  name: z.string().regex(/^[a-z0-9-]+$/, "Name must be lowercase letters, numbers, and hyphens only"),
});

type CreateEnvironmentFormValues = z.infer<typeof createEnvironmentSchema>;

export function CreateEnvironmentDialog({
  projectId,
  open,
  onOpenChange,
}: {
  projectId: string;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const createEnvironment = useCreateEnvironment(projectId);
  const [formError, setFormError] = useState<string | null>(null);

  const {
    register,
    handleSubmit,
    reset,
    setError,
    formState: { errors },
  } = useForm<CreateEnvironmentFormValues>({ resolver: zodResolver(createEnvironmentSchema) });

  const handleOpenChange = (next: boolean) => {
    if (!next) {
      reset();
      setFormError(null);
    }
    onOpenChange(next);
  };

  const onSubmit = async (values: CreateEnvironmentFormValues) => {
    setFormError(null);
    try {
      await createEnvironment.mutateAsync(values);
      toast.success("Environment created");
      reset();
      onOpenChange(false);
    } catch (err) {
      if (err instanceof ApiError && err.status === 409) {
        setError("name", { message: "environment already exists" });
      } else if (err instanceof ApiError) {
        setFormError(err.message);
      } else {
        setFormError("Something went wrong. Please try again.");
      }
    }
  };

  return (
    <Dialog open={open} onOpenChange={handleOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>New environment</DialogTitle>
        </DialogHeader>
        <form className="flex flex-col gap-4" onSubmit={handleSubmit(onSubmit)}>
          {formError && (
            <div role="alert" className="rounded-lg bg-destructive/10 px-3 py-2 text-sm text-destructive">
              {formError}
            </div>
          )}
          <div className="flex flex-col gap-1.5">
            <Label htmlFor="name">Name</Label>
            <Input id="name" autoComplete="off" {...register("name")} />
            {errors.name && <p className="text-sm text-destructive">{errors.name.message}</p>}
          </div>
          <DialogFooter>
            <Button type="submit" disabled={createEnvironment.isPending}>
              {createEnvironment.isPending ? "Creating…" : "Create"}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
