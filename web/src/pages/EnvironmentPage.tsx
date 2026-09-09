import { useState } from "react";
import { Link, useParams } from "react-router-dom";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import { Label } from "@/components/ui/label";
import { ValueField } from "@/components/ValueField";
import { useEnvironment, useSaveValues, useValues } from "@/hooks/useValues";
import { useSchema } from "@/hooks/useSchema";
import type { SchemaField } from "@/hooks/useSchema";
import { ApiError } from "@/lib/api";

function initFormValues(fields: SchemaField[], current: Record<string, unknown> | undefined): Record<string, unknown> {
  const result: Record<string, unknown> = {};
  for (const field of fields) {
    const existing = current?.[field.name];
    if (field.type === "bool") {
      result[field.name] = typeof existing === "boolean" ? existing : Boolean(field.default ?? false);
      continue;
    }
    if (field.type === "json") {
      const source = existing !== undefined ? existing : field.default;
      result[field.name] = source !== undefined ? JSON.stringify(source) : "";
      continue;
    }
    if (existing !== undefined) {
      result[field.name] = String(existing);
    } else if (field.default !== undefined) {
      result[field.name] = String(field.default);
    } else {
      result[field.name] = "";
    }
  }
  return result;
}

function fieldNamedInMessage(fields: SchemaField[], message: string): string | null {
  const match = fields.find((f) => message.includes(f.name));
  return match ? match.name : null;
}

export function EnvironmentPage() {
  const { envId } = useParams<{ envId: string }>();
  const id = envId ?? "";
  const environment = useEnvironment(id);

  if (environment.isPending) {
    return <div className="text-sm text-muted-foreground">Loading…</div>;
  }

  const notFound = environment.isError && environment.error instanceof ApiError && environment.error.status === 404;
  if (notFound) {
    return <div className="text-sm text-muted-foreground">Environment not found</div>;
  }

  if (environment.isError || !environment.data) {
    return (
      <div role="alert" className="rounded-lg bg-destructive/10 px-3 py-2 text-sm text-destructive">
        Failed to load environment.
      </div>
    );
  }

  return (
    <EnvironmentEditor
      environmentId={id}
      projectId={environment.data.projectId}
      environmentName={environment.data.name}
    />
  );
}

function EnvironmentEditor({
  environmentId,
  projectId,
  environmentName,
}: {
  environmentId: string;
  projectId: string;
  environmentName: string;
}) {
  const schema = useSchema(projectId);
  const values = useValues(environmentId);
  const saveValues = useSaveValues(environmentId);

  // `edits` holds only what the user has changed in this session; anything
  // not yet touched falls back to the server-derived initial value below.
  // This avoids syncing async query data into state via an effect.
  const [edits, setEdits] = useState<Record<string, unknown>>({});
  const [formError, setFormError] = useState<string | null>(null);
  const [fieldErrors, setFieldErrors] = useState<Record<string, string>>({});

  const fields = schema.data?.definition.fields ?? [];

  const isPending = schema.isPending || values.isPending;
  const schemaNotFound = schema.isError && schema.error instanceof ApiError && schema.error.status === 404;
  const otherSchemaError = schema.isError && !schemaNotFound;
  const isError = otherSchemaError || values.isError;

  if (isPending) {
    return <div className="text-sm text-muted-foreground">Loading…</div>;
  }

  if (schemaNotFound) {
    return (
      <div className="flex flex-col gap-3 text-sm text-muted-foreground">
        <p>Define a schema first.</p>
        <Link to={`/projects/${projectId}/schema`} className="text-primary hover:underline">
          Go to schema builder
        </Link>
      </div>
    );
  }

  if (isError || !schema.data) {
    return (
      <div role="alert" className="rounded-lg bg-destructive/10 px-3 py-2 text-sm text-destructive">
        Failed to load environment values.
      </div>
    );
  }

  const initialValues = initFormValues(fields, values.data?.values);
  const formValues: Record<string, unknown> = { ...initialValues, ...edits };

  const updateField = (name: string, next: unknown) => {
    setEdits((prev) => ({ ...prev, [name]: next }));
  };

  const onSave = async () => {
    setFormError(null);
    setFieldErrors({});

    const payload: Record<string, unknown> = {};
    for (const field of fields) {
      const raw = formValues[field.name];

      if (field.type === "bool") {
        payload[field.name] = Boolean(raw);
        continue;
      }

      if (field.type === "json") {
        const text = typeof raw === "string" ? raw.trim() : "";
        if (text === "") continue;
        try {
          payload[field.name] = JSON.parse(text);
        } catch {
          setFieldErrors((prev) => ({ ...prev, [field.name]: "Invalid JSON." }));
          setFormError(`"${field.name}" is not valid JSON.`);
          return;
        }
        continue;
      }

      if (field.type === "int" || field.type === "float") {
        const text = typeof raw === "string" ? raw.trim() : "";
        if (text === "") continue;
        const num = Number(text);
        const invalid = Number.isNaN(num) || (field.type === "int" && !Number.isInteger(num));
        if (invalid) {
          const message = field.type === "int" ? "Must be a whole number." : "Must be a number.";
          setFieldErrors((prev) => ({ ...prev, [field.name]: message }));
          setFormError(`"${field.name}" ${message.toLowerCase()}`);
          return;
        }
        payload[field.name] = num;
        continue;
      }

      // string, duration, enum
      const text = typeof raw === "string" ? raw : "";
      if (text === "") continue;
      payload[field.name] = text;
    }

    try {
      await saveValues.mutateAsync({ values: payload });
      toast.success("Values saved");
    } catch (err) {
      if (err instanceof ApiError && err.status === 422) {
        setFormError(err.message);
        const named = fieldNamedInMessage(fields, err.message);
        if (named) setFieldErrors((prev) => ({ ...prev, [named]: err.message }));
      } else if (err instanceof ApiError) {
        setFormError(err.message);
      } else {
        setFormError("Something went wrong. Please try again.");
      }
    }
  };

  return (
    <div className="flex flex-col gap-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-xl font-semibold">{environmentName}</h1>
          <p className="text-sm text-muted-foreground">Values · schema v{schema.data.schemaVersion}</p>
        </div>
        <Link to={`/projects/${projectId}`} className="text-sm text-muted-foreground hover:text-foreground">
          Back to project
        </Link>
      </div>

      {formError && (
        <div role="alert" className="rounded-lg bg-destructive/10 px-3 py-2 text-sm text-destructive">
          {formError}
        </div>
      )}

      {fields.length === 0 && <div className="text-sm text-muted-foreground">This schema has no fields yet.</div>}

      {fields.length > 0 && (
        <div className="flex flex-col gap-4">
          {fields.map((field) => (
            <div key={field.name} className="flex flex-col gap-1.5">
              <Label htmlFor={field.name}>
                {field.name}
                {field.required && <span className="text-destructive"> *</span>}
              </Label>
              <ValueField
                id={field.name}
                field={field}
                value={formValues[field.name]}
                onChange={(next) => updateField(field.name, next)}
              />
              {fieldErrors[field.name] && <p className="text-sm text-destructive">{fieldErrors[field.name]}</p>}
              {field.description && <p className="text-sm text-muted-foreground">{field.description}</p>}
            </div>
          ))}
        </div>
      )}

      <div>
        <Button onClick={onSave} disabled={saveValues.isPending}>
          {saveValues.isPending ? "Saving…" : "Save"}
        </Button>
      </div>
    </div>
  );
}
