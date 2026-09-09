import { useState } from "react";
import { ChevronDownIcon } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Textarea } from "@/components/ui/textarea";
import { cn } from "cn";

// v1 targeting editor: a collapsible raw-JSON escape hatch per field. The
// caller owns the raw text (mirrors how json-typed ValueField works) so the
// parent can defer real JSON.parse validation to save time and map 422s
// from the server back onto the right section.
export function TargetingEditor({
  fieldName,
  value,
  onChange,
  error,
}: {
  fieldName: string;
  value: string;
  onChange: (next: string) => void;
  error?: string;
}) {
  const hasContent = value.trim() !== "";
  const [open, setOpen] = useState(hasContent || Boolean(error));
  // `open` only reflects the user's manual toggle (or the mount-time initial
  // state). A 422 sets `error` on an already-mounted, possibly-collapsed
  // instance — it does not remount (that only happens on a successful save,
  // see EnvironmentPage's `key={values.data?.version}`) — so the section
  // must also show whenever there's an error, regardless of `open`.
  const expanded = open || Boolean(error);

  let ruleCount: number | null = 0;
  let parseError: string | null = null;
  if (hasContent) {
    try {
      const parsed = JSON.parse(value);
      if (Array.isArray(parsed)) {
        ruleCount = parsed.length;
      } else {
        ruleCount = null;
        parseError = "Targeting rules must be a JSON array.";
      }
    } catch {
      ruleCount = null;
      parseError = "Invalid JSON.";
    }
  }

  return (
    <div className="rounded-lg border border-border">
      <Button
        type="button"
        variant="ghost"
        size="sm"
        aria-expanded={expanded}
        onClick={() => setOpen((prev) => !prev)}
        className="w-full justify-between rounded-lg"
      >
        <span>
          Targeting
          {ruleCount !== null && ruleCount > 0 && (
            <span className="text-muted-foreground"> ({ruleCount} rule{ruleCount === 1 ? "" : "s"})</span>
          )}
        </span>
        <ChevronDownIcon className={cn("size-4 transition-transform", expanded && "rotate-180")} />
      </Button>
      {expanded && (
        <div className="flex flex-col gap-1.5 border-t border-border p-3">
          <Textarea
            aria-label={`${fieldName} targeting rules`}
            value={value}
            onChange={(e) => onChange(e.target.value)}
            placeholder={'[{"conditions":[{"attribute":"country","operator":"in","values":["US"]}],"value":true}]'}
            className="font-mono text-xs"
            rows={6}
          />
          {parseError && <p className="text-sm text-destructive">{parseError}</p>}
          {error && <p className="text-sm text-destructive">{error}</p>}
          <p className="text-xs text-muted-foreground">
            Raw JSON array of rules, evaluated in order. Each rule takes optional <code>conditions</code> plus
            exactly one of <code>value</code> or <code>rollout</code>. Leave empty for no targeting.
          </p>
        </div>
      )}
    </div>
  );
}
