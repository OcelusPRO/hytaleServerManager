// Shapes of the manager API responses.

// "sleeping": stopped while empty, restarted as soon as a player connects.
export type ServerState = "stopped" | "running" | "stopping" | "crashed" | "sleeping";

export interface ServerStatus {
  state: ServerState;
  pid?: number;
  saveId?: string;
  startedAt?: string;
  exitCode?: number;
  activeSaveId: string;
  installing: boolean;
  playerCount: number;
  hostWarnings: HostWarning[];
}

/** A host problem reported by the game server (see internal/hostcheck). */
export interface HostWarning {
  kind: "udpReceiveBuffer" | string;
  values?: Record<string, number>;
  seenAt: string;
}

export type MetricsRange = "1h" | "6h" | "24h" | "7d" | "14d";

/** One period of the resource history (see internal/metrics). */
export interface MetricPoint {
  t: number; // Unix seconds
  cpu?: number; // cores used (1.5 = 150 %)
  memMb?: number;
  memMaxMb?: number;
  ccpu?: number; // whole container
  cmem?: number;
  cmemMax?: number;
  players: number;
  running: number; // seconds per state
  sleeping: number;
  stopped: number;
}

export interface MetricsSeries {
  from: number;
  to: number;
  step: number;
  points: MetricPoint[] | null;
  summary: {
    runningSeconds: number;
    sleepingSeconds: number;
    stoppedSeconds: number;
    peakMemMb?: number;
    avgCpu?: number;
    containerPeakMemMb?: number;
    containerAvgCpu?: number;
    /** Container usage per server state. */
    states?: Partial<Record<"running" | "sleeping" | "stopped", { cpu?: number; memMb?: number }>>;
    peakPlayers: number;
  };
  cpuCount: number;
  processMetrics: boolean;
  containerMetrics: boolean;
}

export type LoginState = "idle" | "pending" | "succeeded" | "failed" | "cancelled";

export interface LoginStatus {
  state: LoginState;
  userCode?: string;
  verificationUri?: string;
  verificationUriComplete?: string;
  expiresAt?: string;
  error?: string;
}

export interface HytaleAuthStatus {
  authenticated: boolean;
  profileUuid?: string;
  profileName?: string;
  refreshedAt?: string;
  login: LoginStatus;
}

export interface HytaleProfile {
  uuid: string;
  username: string;
}

export type SetupActivity = "idle" | "java" | "checking" | "downloading" | "extracting";

export interface SetupStatus {
  javaReady: boolean;
  javaVersion?: string;
  gameReady: boolean;
  gameVersion?: string;
  latestVersion?: string;
  updateAvailable: boolean;
  /** Build the game is frozen on, empty when following the latest. */
  pinnedVersion: string;
  /** Previous builds available for rollback, newest first. */
  storedVersions: string[];
  waitingForAccount: boolean;
  activity: SetupActivity;
  /** Completed fraction of the activity, -1 when unknown. */
  progress: number;
  error?: string;
}

export interface Save {
  id: string;
  name: string;
  createdAt: string;
  sizeBytes: number;
}

export interface SavesResponse {
  saves: Save[];
  activeSaveId: string;
  runningSaveId?: string;
}

export interface CurseForgeLink {
  projectId: number;
  fileId: number;
  name: string;
  url?: string;
  fileDate: string;
  installedAt: string;
  pack?: string;
}

export interface Mod {
  fileName: string;
  enabled: boolean;
  sizeBytes: number;
  modifiedAt: string;
  curseforge?: CurseForgeLink;
}

export interface CurseForgeStatus {
  configured: boolean;
  keyHint?: string;
  /** Hytale was found in the CurseForge catalogue. */
  ready: boolean;
  modpacks: boolean;
  error?: string;
}

export interface CurseForgeFile {
  id: number;
  displayName: string;
  fileName: string;
  date: string;
  sizeBytes: number;
  releaseType: "release" | "beta" | "alpha";
  gameVersions?: string[];
  downloadable: boolean;
}

export interface CurseForgeProject {
  id: number;
  name: string;
  summary: string;
  author?: string;
  downloads: number;
  logoUrl?: string;
  url?: string;
  latest?: CurseForgeFile;
}

export interface CurseForgeSearch {
  items: CurseForgeProject[];
  page: number;
  pageSize: number;
  totalCount: number;
}

export interface CurseForgeUpdate {
  fileName: string;
  name: string;
  projectId: number;
  currentDate: string;
  latest: CurseForgeFile;
}

export interface CurseForgeInstallResult {
  installed: string[];
  warnings?: string[];
}

export interface CurseForgeJob {
  id: string;
  title: string;
  saveId: string;
  state: "running" | "done" | "failed";
  progress: number;
  message?: string;
  warnings?: string[];
  error?: string;
  startedAt: string;
  finishedAt?: string;
}

export interface Backup {
  id: string;
  saveId: string;
  createdAt: string;
  sizeBytes: number;
  /** Autosave still subject to rotation (naming one turns it into a manual backup). */
  auto: boolean;
  name: string;
  origin: "manual" | "autosave" | "restore" | "update";
  /** Origin precision, e.g. the id of the backup being restored. */
  detail?: string;
  note?: string;
  slot?: number;
}

export type ConsoleStream = "out" | "err" | "in" | "sys";

export interface ConsoleLine {
  seq: number;
  time: string;
  stream: ConsoleStream;
  text: string;
}

export interface OnlinePlayer {
  uuid: string;
  name: string;
  joinedAt: string;
}

export interface PlayerRef {
  uuid: string;
  /** Empty when the server files do not tell the name. */
  name: string;
}

export interface BanEntry {
  player: PlayerRef;
  by?: PlayerRef;
  reason?: string;
  timestamp?: string;
  expiresOn?: string;
}

export interface PlayersResponse {
  /** True when the server runs the active world (additions are possible). */
  live: boolean;
  online: OnlinePlayer[];
  whitelist: { enabled: boolean; players: PlayerRef[] };
  bans: BanEntry[];
  operators: PlayerRef[];
}

export type AccessList = "bans" | "operators" | "whitelist";

export type AuthMode = "authenticated" | "offline";
export type Patchline = "release" | "pre-release";

export interface ServerSettings {
  activeSaveId: string;
  minMemoryMb: number;
  maxMemoryMb: number;
  port: number;
  patchline: Patchline;
  authMode: AuthMode;
  useAotCache: boolean;
  disableSentry: boolean;
  autoStart: boolean;
  autoRestart: boolean;
  nativeBackups: boolean;
  nativeBackupMinutes: number;
  stopTimeoutSeconds: number;
  backupBeforeRestore: boolean;
  acceptEarlyPlugins: boolean;
  autosaveEnabled: boolean;
  autosaveMinutes: number;
  autosaveSlots: number;
  autoUpdate: boolean;
  autoUpdateIdleMinutes: number;
  autoUpdateForceHours: number;
  sleepEnabled: boolean;
  sleepIdleMinutes: number;
  gameLanguage: "en" | "fr";
  gameVersionPin: string;
}

export type PlayerAction = "kick" | "ban" | "unban" | "op" | "deop" | "whitelist-add" | "whitelist-remove";

export type ConfigKind = "server" | "world" | "mod";

export interface ConfigFile {
  id: string;
  kind: ConfigKind;
  label: string;
  group?: string;
}

export type JsonValue = string | number | boolean | null | JsonValue[] | { [key: string]: JsonValue };
export type JsonObject = { [key: string]: JsonValue };
