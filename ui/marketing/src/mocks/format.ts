export function formatIDR(amount: number): string {
  return `Rp ${String(Math.round(amount)).replace(/\B(?=(?:\d{3})+(?!\d))/g, ".")}`;
}
