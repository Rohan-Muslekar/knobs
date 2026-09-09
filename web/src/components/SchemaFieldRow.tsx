import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Switch } from "@/components/ui/switch";
import { TableCell, TableRow } from "@/components/ui/table";
import type { FieldType, SchemaField } from "@/hooks/useSchema";

const FIELD_TYPES: FieldType[] = ["string", "int", "float", "bool", "json", "enum", "duration"];

export function SchemaFieldRow({
  field,
  index,
  onChange,
  onRemove,
}: {
  field: SchemaField;
  index: number;
  onChange: (index: number, field: SchemaField) => void;
  onRemove: (index: number) => void;
}) {
  const update = (patch: Partial<SchemaField>) => onChange(index, { ...field, ...patch });

  return (
    <TableRow data-testid={`field-row-${index}`}>
      <TableCell>
        <Input
          aria-label="Field name"
          value={field.name}
          onChange={(e) => update({ name: e.target.value })}
          placeholder="fieldName"
          autoComplete="off"
        />
      </TableCell>
      <TableCell>
        <Select
          value={field.type}
          onValueChange={(value) =>
            update({
              type: value as FieldType,
              min: undefined,
              max: undefined,
              pattern: undefined,
              enumValues: undefined,
            })
          }
        >
          <SelectTrigger aria-label="Field type">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {FIELD_TYPES.map((type) => (
              <SelectItem key={type} value={type}>
                {type}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </TableCell>
      <TableCell>
        <Switch
          aria-label="Required"
          checked={field.required}
          onCheckedChange={(checked) => update({ required: checked })}
        />
      </TableCell>
      <TableCell>
        {(field.type === "int" || field.type === "float") && (
          <div className="flex gap-2">
            <Input
              type="number"
              aria-label="Minimum"
              placeholder="min"
              value={field.min ?? ""}
              onChange={(e) => update({ min: e.target.value === "" ? undefined : Number(e.target.value) })}
              className="w-20"
            />
            <Input
              type="number"
              aria-label="Maximum"
              placeholder="max"
              value={field.max ?? ""}
              onChange={(e) => update({ max: e.target.value === "" ? undefined : Number(e.target.value) })}
              className="w-20"
            />
          </div>
        )}
        {field.type === "string" && (
          <Input
            aria-label="Pattern"
            placeholder="regex pattern"
            value={field.pattern ?? ""}
            onChange={(e) => update({ pattern: e.target.value || undefined })}
          />
        )}
        {field.type === "enum" && (
          <Input
            aria-label="Enum values"
            placeholder="comma-separated values"
            value={(field.enumValues ?? []).join(", ")}
            onChange={(e) =>
              update({
                enumValues: e.target.value
                  .split(",")
                  .map((v) => v.trim())
                  .filter((v) => v.length > 0),
              })
            }
          />
        )}
      </TableCell>
      <TableCell>
        <Input
          aria-label="Description"
          value={field.description ?? ""}
          onChange={(e) => update({ description: e.target.value || undefined })}
          placeholder="optional"
          autoComplete="off"
        />
      </TableCell>
      <TableCell>
        <Button variant="outline" size="sm" onClick={() => onRemove(index)}>
          Remove
        </Button>
      </TableCell>
    </TableRow>
  );
}
