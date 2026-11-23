import { formatIDR } from "./format";

export function signed(amount: number, kind: "expense" | "income" | "transfer"): string {
  if (kind === "income") return `+${formatIDR(amount)}`;
  if (kind === "expense") return `−${formatIDR(amount)}`;
  return formatIDR(amount);
}
