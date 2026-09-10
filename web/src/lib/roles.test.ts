import { describe, it, expect } from "vitest";
import { roleAtLeast } from "@/lib/roles";

describe("roleAtLeast", () => {
  it("orders roles viewer < editor < admin < owner", () => {
    expect(roleAtLeast("viewer", "viewer")).toBe(true);
    expect(roleAtLeast("viewer", "editor")).toBe(false);
    expect(roleAtLeast("editor", "viewer")).toBe(true);
    expect(roleAtLeast("editor", "editor")).toBe(true);
    expect(roleAtLeast("editor", "admin")).toBe(false);
    expect(roleAtLeast("admin", "editor")).toBe(true);
    expect(roleAtLeast("admin", "admin")).toBe(true);
    expect(roleAtLeast("admin", "owner")).toBe(false);
    expect(roleAtLeast("owner", "admin")).toBe(true);
    expect(roleAtLeast("owner", "owner")).toBe(true);
  });

  it("treats a missing role as below every minimum", () => {
    expect(roleAtLeast(null, "viewer")).toBe(false);
    expect(roleAtLeast(undefined, "viewer")).toBe(false);
  });
});
