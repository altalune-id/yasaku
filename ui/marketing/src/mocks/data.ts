import type { SiteCopy } from "../content";

type Mocks = SiteCopy["mocks"];
type CategoryKey = keyof Mocks["categories"];
type WalletKey = keyof Mocks["wallets"];

export const slot = {
  primary: {
    dot: "bg-primary",
    text: "text-primary",
    tint: "bg-primary/15",
    progress: "progress-primary",
  },
  accent: {
    dot: "bg-accent",
    text: "text-accent",
    tint: "bg-accent/15",
    progress: "progress-accent",
  },
  info: { dot: "bg-info", text: "text-info", tint: "bg-info/15", progress: "progress-info" },
  success: {
    dot: "bg-success",
    text: "text-success",
    tint: "bg-success/15",
    progress: "progress-success",
  },
  error: { dot: "bg-error", text: "text-error", tint: "bg-error/15", progress: "progress-error" },
  secondary: {
    dot: "bg-secondary",
    text: "text-secondary",
    tint: "bg-secondary/15",
    progress: "progress-secondary",
  },
} as const;

export type Slot = keyof typeof slot;

export const spending: { key: CategoryKey; amount: number; slot: Slot }[] = [
  { key: "food", amount: 1_850_000, slot: "primary" },
  { key: "bills", amount: 1_420_000, slot: "accent" },
  { key: "transport", amount: 980_000, slot: "info" },
  { key: "shopping", amount: 760_000, slot: "success" },
  { key: "phone", amount: 390_000, slot: "error" },
  { key: "other", amount: 840_000, slot: "secondary" },
];

export const moneyIn = 9_700_000;
export const moneyOut = spending.reduce((sum, s) => sum + s.amount, 0);

export const cashflow = [
  { in: 9_200_000, out: 6_800_000 },
  { in: 9_400_000, out: 7_100_000 },
  { in: 9_500_000, out: 5_900_000 },
  { in: moneyIn, out: moneyOut },
];

export const wallets: { key: WalletKey; balance: number }[] = [
  { key: "bank", balance: 8_400_000 },
  { key: "cash", balance: 1_225_000 },
  { key: "ewallet", balance: 310_000 },
];

export const movement: { key: WalletKey; opening: number; into: number; out: number }[] = [
  { key: "bank", opening: 4_940_000, into: 9_700_000, out: 6_240_000 },
  { key: "cash", opening: 1_225_000, into: 1_540_000, out: 1_540_000 },
  { key: "ewallet", opening: 310_000, into: 1_250_000, out: 1_250_000 },
];

export function share(amount: number): number {
  return Math.round((amount / moneyOut) * 100);
}

export const incomeFlows: { from: CategoryKey; wallet: WalletKey; amount: number }[] = [
  { from: "salary", wallet: "bank", amount: 8_500_000 },
  { from: "freelance", wallet: "bank", amount: 1_200_000 },
];

export const spendFlows: { wallet: WalletKey; to: CategoryKey; amount: number }[] = [
  { wallet: "bank", to: "food", amount: 300_000 },
  { wallet: "bank", to: "bills", amount: 1_420_000 },
  { wallet: "bank", to: "shopping", amount: 580_000 },
  { wallet: "bank", to: "phone", amount: 390_000 },
  { wallet: "bank", to: "other", amount: 760_000 },
  { wallet: "cash", to: "food", amount: 1_100_000 },
  { wallet: "cash", to: "transport", amount: 360_000 },
  { wallet: "cash", to: "other", amount: 80_000 },
  { wallet: "ewallet", to: "food", amount: 450_000 },
  { wallet: "ewallet", to: "transport", amount: 620_000 },
  { wallet: "ewallet", to: "shopping", amount: 180_000 },
];

export const homeDays: {
  label: "today" | "yesterday";
  rows: {
    title: keyof Mocks["tx"];
    category: CategoryKey;
    wallet: WalletKey;
    amount: number;
    income?: boolean;
  }[];
}[] = [
  {
    label: "today",
    rows: [
      { title: "coffee", category: "food", wallet: "cash", amount: 25_000 },
      { title: "ride", category: "transport", wallet: "ewallet", amount: 32_000 },
    ],
  },
  {
    label: "yesterday",
    rows: [
      { title: "salary", category: "salary", wallet: "bank", amount: 8_500_000, income: true },
      { title: "electricity", category: "bills", wallet: "bank", amount: 450_000 },
    ],
  },
];
