export type FeatureVariant =
  | "search"
  | "wallets"
  | "categories"
  | "transfer"
  | "adjust"
  | "ledgers";

export interface SiteCopy {
  meta: { title: string; description: string; ogTitle: string; ogDescription: string };
  nav: {
    links: { label: string; href: string }[];
    cta: string;
    themeLabel: string;
    pauseLabel: string;
    playLabel: string;
    languageLabel: string;
    githubLabel: string;
    menuLabel: string;
    skipLabel: string;
    localeLabel: string;
  };
  hero: { eyebrow: string; title: string; sub: string; primaryCta: string; secondaryCta: string };
  features: {
    eyebrow: string;
    title: string;
    cards: { title: string; body: string; visual: FeatureVariant }[];
  };
  payday: {
    eyebrow: string;
    title: string;
    body: string;
    steps: { title: string; body: string }[];
  };
  ai: {
    eyebrow: string;
    title: string;
    body: string;
    rotatorPrefix: string;
    rotatorNames: string[];
    note: string;
    stepsTitle: string;
    setup: {
      groupLabel: string;
      chat: {
        tab: string;
        label: string;
        body: string;
        copy: string;
        success: string;
        idle: string;
      };
      coding: { tab: string; label: string; body: string; copy: string; success: string };
    };
    steps: { title: string; body: string; typed: string }[];
  };
  openSource: {
    eyebrow: string;
    title: string;
    body: string;
    license: string;
    github: string;
    cloud: { title: string; body: string; points: string[]; cta: string };
    selfHost: {
      title: string;
      body: string;
      items: { title: string; detail: string; detailHref?: string }[];
      note: string;
      docs: string;
    };
  };
  faq: { title: string; items: { q: string; a: string }[] };
  finalCta: { title: string; cta: string; note: string };
  copyBar: { value: string; copied: string; manual: string };
  footer: {
    tagline: string;
    columnTitles: { apps: string; legal: string; resources: string };
    terms: string;
    privacy: string;
    github: string;
    copyright: string;
    trademarks: string;
  };
  mocks: {
    showcaseAlt: string;
    homeAlt: string;
    periodsAlt: string;
    home: string;
    whereMoneyWent: string;
    saved: string;
    open: string;
    periodsTitle: string;
    nextPeriod: string;
    periodRanges: string[];
    periodAlt: string;
    chatAlt: string;
    reports: string;
    wallet: string;
    opening: string;
    total: string;
    period: string;
    netPeriod: string;
    moneyIn: string;
    moneyOut: string;
    net: string;
    vsLast: string;
    cashflow: string;
    today: string;
    yesterday: string;
    totalAcross: string;
    categories: Record<
      | "health"
      | "food"
      | "transport"
      | "shopping"
      | "bills"
      | "phone"
      | "other"
      | "salary"
      | "freelance",
      string
    >;
    wallets: Record<"cash" | "bank" | "ewallet", string>;
    tx: Record<"coffee" | "ride" | "salary" | "electricity" | "topUp", string>;
    transfer: string;
    userMessage: string;
    reply: string;
    expense: string;
    preview: string;
    continueWith: string;
    toolLabel: string;
    walletBalances: string;
    reopen: string;
    reopenHint: string;
    confirm: string;
    cancel: string;
    closeMonth: string;
    closed: string;
    whatGetsLocked: string;
    entries: string;
    startingBalances: string;
    adjustBalance: string;
    newWallet: string;
    realBalance: string;
    balanceFix: string;
    quickAdd: string;
    searchQuery: string;
    searchCount: string;
    searchDays: string[];
    ledgers: string[];
    ledgerMembers: string[];
  };
}
