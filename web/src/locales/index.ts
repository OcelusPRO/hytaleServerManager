// All interface messages, assembled from the sections. Each section holds
// the English text and its translations; the English shape is the reference.
import * as common from "./sections/common";
import * as configFields from "./sections/configFields";
import * as console from "./sections/console";
import * as errors from "./sections/errors";
import * as features from "./sections/features";
import * as hytale from "./sections/hytale";
import * as pages from "./sections/pages";
import * as players from "./sections/players";
import * as settings from "./sections/settings";
import * as shell from "./sections/shell";
import * as worlds from "./sections/worlds";
import type { Shape } from "./shape";

export const en = {
  common: common.en,
  configFields: configFields.en,
  console: console.en,
  errors: errors.en,
  features: features.en,
  hytale: hytale.en,
  pages: pages.en,
  players: players.en,
  settings: settings.en,
  shell: shell.en,
  worlds: worlds.en,
};

export type Messages = Shape<typeof en>;

export const fr: Messages = {
  common: common.fr,
  configFields: configFields.fr,
  console: console.fr,
  errors: errors.fr,
  features: features.fr,
  hytale: hytale.fr,
  pages: pages.fr,
  players: players.fr,
  settings: settings.fr,
  shell: shell.fr,
  worlds: worlds.fr,
};
