import type { Locale } from "../site";
import { en } from "./en";
import { id } from "./id";
import type { SiteCopy } from "./types";

export type { SiteCopy } from "./types";

export const copy: Record<Locale, SiteCopy> = { en, id };
