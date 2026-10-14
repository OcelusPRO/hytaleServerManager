// Presentation hints for well-known configuration keys. Unknown keys are
// still editable: the form is derived from the JSON values themselves.

import type { ConfigKind, JsonObject, JsonValue } from "./types";

export interface FieldMeta {
  label?: string;
  hint?: string;
  min?: number;
  max?: number;
  step?: number;
  options?: { value: string; label: string }[];
  password?: boolean;
  /** Shown but not editable (internal values). */
  readOnly?: boolean;
}

const server: Record<string, FieldMeta> = {
  ServerName: { label: "Nom du serveur", hint: "Nom public affiché dans la liste des serveurs." },
  MOTD: { label: "Message du jour", hint: "Affiché sous le nom du serveur." },
  Password: { label: "Mot de passe", hint: "Vide = serveur public. Une valeur rend le serveur privé.", password: true },
  MaxPlayers: { label: "Joueurs maximum", hint: "Plus de joueurs demande plus de RAM et de CPU.", min: 1 },
  MaxViewRadius: { label: "Distance de vue (chunks)", hint: "Recommandé : 12 (standard) à 16 (élevé).", min: 2, max: 32 },
  LocalCompressionEnabled: { label: "Compression locale", hint: "Recommandée pour les serveurs publics." },
  Version: { label: "Version du fichier", readOnly: true },
  Defaults: { label: "Valeurs par défaut" },
  "Defaults.World": { label: "Monde par défaut", hint: "Créé au lancement s'il n'existe pas dans universe/." },
  "Defaults.GameMode": {
    label: "Mode de jeu",
    options: [
      { value: "Adventure", label: "Aventure" },
      { value: "Creative", label: "Créatif" },
    ],
  },
  ConnectionTimeouts: { label: "Délais de connexion", hint: "Vide = valeurs par défaut du moteur." },
  RateLimit: { label: "Limitation de débit", hint: "Protège contre le spam de paquets. Laissez vide si vous ne connaissez pas les limites exactes." },
  PlayerStorage: { label: "Stockage des joueurs" },
  "PlayerStorage.Type": { label: "Type" },
  DisplayTmpTagsInStrings: { label: "Afficher les balises temporaires", hint: "Outil de débogage : laissez désactivé en production." },
  RequireJoinPermission: { label: "Liste blanche (permission de connexion requise)" },
};

const world: Record<string, FieldMeta> = {
  Seed: { label: "Graine", hint: "Partagez-la pour partager la carte." },
  WorldGen: { label: "Génération du monde" },
  "WorldGen.Type": { label: "Générateur", hint: "« Hytale » est le générateur standard." },
  UUID: { label: "Identifiant interne", readOnly: true },
  IsPvpEnabled: { label: "JcJ (PvP) activé", hint: "Désactivé : plus de dégâts entre joueurs." },
  IsFallDamageEnabled: { label: "Dégâts de chute", hint: "À désactiver pour un lobby ou un parcours." },
  IsGameTimePaused: { label: "Temps figé", hint: "Activé : le soleil reste en place." },
  GameTimeDate: { label: "Date et heure du jeu", hint: "Format ISO, ex. 0001-01-01T05:30:00Z (5h30)." },
  ClientEffects: { label: "Effets visuels" },
  "ClientEffects.SunIntensity": { label: "Intensité du soleil", min: 0, max: 1, step: 0.05 },
  "ClientEffects.BloomIntensity": { label: "Intensité du halo lumineux", min: 0, max: 1, step: 0.05 },
  "ClientEffects.SunAngleDegrees": { label: "Angle du soleil (°)", hint: "Définit l'angle des ombres.", step: 1 },
  IsSpawningNPC: { label: "Apparition des PNJ", hint: "Désactivé : le monde reste vide sauf apparition manuelle." },
  IsAllNPCFrozen: { label: "Figer tous les PNJ", hint: "Désactive l'IA (débogage, captures d'écran)." },
  IsSavingPlayers: { label: "Sauvegarde des joueurs", hint: "Désactivé : inventaire et position réinitialisés à chaque redémarrage." },
  IsUnloadingChunks: { label: "Déchargement des chunks", hint: "Recommandé pour économiser la RAM." },
  ChunkStorage: { label: "Stockage des chunks" },
  "ChunkStorage.Type": { label: "Type" },
};

export function fieldMeta(kind: ConfigKind, path: string[]): FieldMeta {
  const table = kind === "server" ? server : kind === "world" ? world : {};
  return table[path.join(".")] ?? {};
}

/** Turns PascalCase / camelCase keys into readable labels. */
export function humanize(key: string): string {
  const spaced = key
    .replace(/[_-]+/g, " ")
    .replace(/([a-z0-9])([A-Z])/g, "$1 $2")
    .replace(/([A-Z]+)([A-Z][a-z])/g, "$1 $2");
  return spaced.charAt(0).toUpperCase() + spaced.slice(1);
}

export interface ConfigChange {
  path: string[];
  value: JsonValue;
}

export function isObject(v: JsonValue | undefined): v is JsonObject {
  return typeof v === "object" && v !== null && !Array.isArray(v);
}

/** Lists the leaf values of next that differ from prev. */
export function diffConfig(prev: JsonObject, next: JsonObject, path: string[] = []): ConfigChange[] {
  const changes: ConfigChange[] = [];
  for (const key of Object.keys(next)) {
    const a = prev[key];
    const b = next[key];
    if (b === undefined) continue;
    if (isObject(a) && isObject(b)) changes.push(...diffConfig(a, b, [...path, key]));
    else if (JSON.stringify(a) !== JSON.stringify(b)) changes.push({ path: [...path, key], value: b });
  }
  return changes;
}

/** Returns a copy of root with value set at path. */
export function setAtPath(root: JsonObject, path: string[], value: JsonValue): JsonObject {
  const [head, ...rest] = path;
  if (head === undefined) return root;
  if (rest.length === 0) return { ...root, [head]: value };
  const child = root[head];
  return { ...root, [head]: setAtPath(isObject(child) ? child : {}, rest, value) };
}
