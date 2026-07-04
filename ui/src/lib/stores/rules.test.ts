import { describe, expect, it, vi } from "vitest";
import { RuleGates } from "./rules.svelte";

function okResponse(body: unknown): Response {
  return new Response(JSON.stringify(body), {
    status: 200,
    headers: { "Content-Type": "application/json" },
  });
}

describe("RuleGates store", () => {
  it("load populates states", async () => {
    const fetchFn = vi.fn().mockResolvedValue(okResponse({ flee: true, attract: true, wind: false }));
    const gates = new RuleGates(fetchFn);
    await gates.load();
    expect(gates.status).toBe("ready");
    expect(gates.states).toEqual({ flee: true, attract: true, wind: false });
    expect(fetchFn).toHaveBeenCalledWith("/boids/rules");
  });

  it("load failure surfaces error", async () => {
    const fetchFn = vi.fn().mockRejectedValue(new Error("backend down"));
    const gates = new RuleGates(fetchFn);
    await gates.load();
    expect(gates.status).toBe("error");
  });

  it("toggle flips optimistically and keeps on success", async () => {
    const fetchFn = vi
      .fn()
      .mockResolvedValueOnce(okResponse({ flee: true, attract: true, wind: true }))
      .mockResolvedValueOnce(okResponse({ kind: "flee", enabled: false }));
    const gates = new RuleGates(fetchFn);
    await gates.load();

    const pending = gates.toggle("flee");
    expect(gates.states.flee).toBe(false); // optimistic
    await pending;
    expect(gates.states.flee).toBe(false); // confirmed
    expect(gates.error).toBeNull();
    expect(fetchFn).toHaveBeenLastCalledWith("/boids/rules/flee", {
      method: "PUT",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ enabled: false }),
    });
  });

  it("toggle reverts on backend rejection", async () => {
    const fetchFn = vi
      .fn()
      .mockResolvedValueOnce(okResponse({ flee: true, attract: true, wind: true }))
      .mockResolvedValueOnce(new Response("nope", { status: 503 }));
    const gates = new RuleGates(fetchFn);
    await gates.load();

    await gates.toggle("flee");
    expect(gates.states.flee).toBe(true); // reverted
    expect(gates.error).not.toBeNull();
  });

  it("toggle reverts on network failure", async () => {
    const fetchFn = vi
      .fn()
      .mockResolvedValueOnce(okResponse({ flee: true, attract: true, wind: true }))
      .mockRejectedValueOnce(new Error("connection refused"));
    const gates = new RuleGates(fetchFn);
    await gates.load();

    await gates.toggle("wind");
    expect(gates.states.wind).toBe(true); // reverted
    expect(gates.error).not.toBeNull();
  });
});
