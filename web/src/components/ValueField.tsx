import { Input } from "@/components/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Switch } from "@/components/ui/switch";
import { Textarea } from "@/components/ui/textarea";
import type { SchemaField } from "@/hooks/useSchema";

export function ValueField({
  id,
  field,
  value,
  onChange,
}: {
  id: string;
  field: SchemaField;
  value: unknown;
  onChange: (value: unknown) => void;
}) {
  switch (field.type) {
    case "bool":
      return (
        <Switch
          id={id}
          aria-label={field.name}
          checked={Boolean(value)}
          onCheckedChange={(checked) => onChange(checked)}
        />
      );
    case "enum":
      return (
        <Select value={typeof value === "string" ? value : ""} onValueChange={(next) => onChange(next)}>
          <SelectTrigger id={id} aria-label={field.name}>
            <SelectValue placeholder="Select a value" />
          </SelectTrigger>
          <SelectContent>
            {(field.enumValues ?? []).map((option) => (
              <SelectItem key={String(option)} value={String(option)}>
                {String(option)}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      );
    case "json":
      return (
        <Textarea
          id={id}
          aria-label={field.name}
          value={typeof value === "string" ? value : ""}
          onChange={(e) => onChange(e.target.value)}
        />
      );
    case "int":
    case "float":
      return (
        <Input
          id={id}
          type="number"
          aria-label={field.name}
          value={typeof value === "string" || typeof value === "number" ? value : ""}
          onChange={(e) => onChange(e.target.value)}
        />
      );
    default:
      // string, duration
      return (
        <Input
          id={id}
          aria-label={field.name}
          value={typeof value === "string" ? value : ""}
          onChange={(e) => onChange(e.target.value)}
        />
      );
  }
}
