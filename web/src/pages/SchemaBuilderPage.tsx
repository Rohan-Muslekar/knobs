import { useState } from "react";
import { Link, useParams } from "react-router-dom";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import { Table, TableBody, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { SchemaFieldRow } from "@/components/SchemaFieldRow";
import { useSchema, useUpdateSchema } from "@/hooks/useSchema";
import type { SchemaField } from "@/hooks/useSchema";
import { ApiError } from "@/lib/api";

// A field carries a client-only `_uid` so list rows can be keyed on stable
// identity instead of array index — otherwise removing a row from the middle
// of the list shifts every index below it and React remounts the wrong
// inputs (losing focus, etc). `_uid` never leaves the browser; it's stripped
// before the fields are sent to the server.
type EditableField = SchemaField & { _uid: string };

let uidCounter = 0;
function nextUid(): string {
  uidCounter += 1;
  return `field-${uidCounter}`;
}

function withUid(field: SchemaField): EditableField {
  return { ...field, _uid: nextUid() };
}

function emptyField(): EditableField {
  return withUid({ name: "", type: "string", required: false });
}

function stripUid(field: EditableField): SchemaField {
  const { _uid, ...rest } = field;
  return rest;
}

function validateFields(fields: SchemaField[]): string | null {
  const names = fields.map((f) => f.name.trim());
  if (names.some((n) => n.length === 0)) return "Every field needs a name.";
  if (new Set(names).size !== names.length) return "Field names must be unique.";
  for (const field of fields) {
    if (field.type === "enum" && (!field.enumValues || field.enumValues.length === 0)) {
      return `Field "${field.name}" is an enum and needs at least one value.`;
    }
    if (field.type === "int" || field.type === "float") {
      if (field.min !== undefined && Number.isNaN(field.min)) {
        return `Field "${field.name}" has an invalid minimum.`;
      }
      if (field.max !== undefined && Number.isNaN(field.max)) {
        return `Field "${field.name}" has an invalid maximum.`;
      }
    }
  }
  return null;
}

export function SchemaBuilderPage() {
  const { projectId } = useParams<{ projectId: string }>();
  const id = projectId ?? "";
  const schema = useSchema(id);

  if (schema.isPending) {
    return <div className="text-sm text-muted-foreground">Loading…</div>;
  }

  if (schema.isError || !schema.data) {
    return (
      <div role="alert" className="rounded-lg bg-destructive/10 px-3 py-2 text-sm text-destructive">
        Failed to load schema.
      </div>
    );
  }

  return <SchemaEditor projectId={id} initialFields={schema.data.definition.fields} schemaVersion={schema.data.schemaVersion} />;
}

function SchemaEditor({
  projectId,
  initialFields,
  schemaVersion,
}: {
  projectId: string;
  initialFields: SchemaField[];
  schemaVersion: number;
}) {
  const updateSchema = useUpdateSchema(projectId);
  const [fields, setFields] = useState<EditableField[]>(() => initialFields.map(withUid));
  const [formError, setFormError] = useState<string | null>(null);

  const updateField = (index: number, next: SchemaField) => {
    setFields((prev) => prev.map((f, i) => (i === index ? { ...next, _uid: f._uid } : f)));
  };

  const removeField = (index: number) => {
    setFields((prev) => prev.filter((_, i) => i !== index));
  };

  const addField = () => {
    setFields((prev) => [...prev, emptyField()]);
  };

  const onSave = async () => {
    setFormError(null);
    const validationError = validateFields(fields);
    if (validationError) {
      setFormError(validationError);
      return;
    }
    try {
      await updateSchema.mutateAsync({ fields: fields.map(stripUid) });
      toast.success("Schema saved");
    } catch (err) {
      if (err instanceof ApiError) {
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
          <h1 className="text-xl font-semibold">Schema</h1>
          <p className="text-sm text-muted-foreground">Version {schemaVersion}</p>
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

      {fields.length === 0 && <div className="text-sm text-muted-foreground">No fields yet</div>}

      {fields.length > 0 && (
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Name</TableHead>
              <TableHead>Type</TableHead>
              <TableHead>Required</TableHead>
              <TableHead>Constraints</TableHead>
              <TableHead>Description</TableHead>
              <TableHead>
                <span className="sr-only">Actions</span>
              </TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {fields.map((field, index) => (
              <SchemaFieldRow key={field._uid} field={field} index={index} onChange={updateField} onRemove={removeField} />
            ))}
          </TableBody>
        </Table>
      )}

      <div className="flex items-center gap-3">
        <Button variant="outline" onClick={addField}>
          Add field
        </Button>
        <Button onClick={onSave} disabled={updateSchema.isPending}>
          {updateSchema.isPending ? "Saving…" : "Save schema"}
        </Button>
      </div>
    </div>
  );
}
