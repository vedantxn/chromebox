/**
 * Baton: cloud browsers for agents. A session is a real Chrome in its own
 * microVM; connect to it with Playwright or Puppeteer over CDP.
 *
 *   const baton = new Baton();                         // reads BATON_API_KEY
 *   const session = await baton.sessions.create();
 *   const browser = await chromium.connectOverCDP(session.connectUrl);
 *   ...
 *   await session.close();
 */

export interface BatonOptions {
  /** Defaults to the BATON_API_KEY environment variable. */
  apiKey?: string;
  /** Defaults to https://batonvm.vercel.app. */
  baseUrl?: string;
}

export interface SessionData {
  id: string;
  status: 'creating' | 'running' | 'migrating' | 'ended' | string;
  node?: string;
  createdAt: string;
  expiresAt?: string;
  endedAt?: string;
  endReason?: string;
  migrations: number;
  /** wss URL for chromium.connectOverCDP / puppeteer.connect. Only on create. */
  connectUrl?: string;
}

export interface Migration {
  from: string;
  to: string;
  /** How long the browser was paused while it moved. */
  pausedMs: number;
  /** How long it was unreachable (pause + network switch). */
  unreachableMs: number;
}

export interface Pause {
  /** How long the browser was paused while its snapshot was taken. */
  pausedMs: number;
  /** Pause request to the browser stored and stopped. */
  totalMs: number;
  /** What was stored, in MiB (memory without zero pages, plus disk changes). */
  storedMiB: number;
}

export interface ProfileInfo {
  name: string;
  createdAt: string;
  storedMiB: number;
}

export interface Fork {
  parent: string;
  /** How long the parent was paused while its snapshot was taken. */
  pausedMs: number;
  /** The copies, each with its own connectUrl. */
  sessions: Session[];
  /** Copies that could not be started (e.g. over a limit). */
  errors: string[];
}

export interface Usage {
  running: number;
  max_running: number;
  paused: number;
  max_paused: number;
  profiles: number;
  max_profiles: number;
  minutes_today: number;
  max_minutes_per_day: number;
  session_lifetime_minutes: number;
}

/** An API error, with the HTTP status (429: over a limit, 503: at capacity). */
export class BatonError extends Error {
  constructor(message: string, readonly status: number) {
    super(message);
    this.name = 'BatonError';
  }
}

export class Session implements SessionData {
  id!: string;
  status!: string;
  node?: string;
  createdAt!: string;
  expiresAt?: string;
  endedAt?: string;
  endReason?: string;
  migrations!: number;
  connectUrl?: string;

  constructor(private readonly client: Baton, data: SessionData) {
    Object.assign(this, data);
  }

  /** End the session and destroy its browser. */
  close(): Promise<void> {
    return this.client.sessions.delete(this.id);
  }

  /** Move the running browser to another machine. Connections stay open. */
  migrate(): Promise<Migration> {
    return this.client.sessions.migrate(this.id);
  }

  /**
   * Save this running browser's state (pages, cookies, storage) as a profile.
   * The session keeps running. `baton.sessions.create({ profile })` then starts
   * new sessions that are already logged in.
   */
  saveProfile(name: string): Promise<{ name: string; pausedMs: number; uploadMs: number; storedMiB: number }> {
    return this.client.profiles.save(this.id, name);
  }

  /**
   * Store this running browser (pages, cookies, memory) and free its machine:
   * a paused session costs nothing and does not count toward your concurrent
   * sessions. Open connections are closed. Call `resume()` later, up to a week.
   */
  pause(): Promise<Pause> {
    return this.client.sessions.pause(this.id);
  }

  /**
   * Bring a paused browser back exactly as it was, on any machine. Updates
   * `connectUrl` (the old one expired with the pause) and returns this session.
   */
  async resume(): Promise<this> {
    const s = await this.client.sessions.resume(this.id);
    Object.assign(this, s);
    return this;
  }

  /**
   * Copy this running browser into `count` new sessions: same open pages,
   * cookies and storage (a logged-in site stays logged in), each its own
   * browser with its own connectUrl. Each copy counts against your limits.
   */
  fork(count = 1): Promise<Fork> {
    return this.client.sessions.fork(this.id, count);
  }

  /** A short-lived URL for watching the browser (CDP screencast). */
  async liveUrl(): Promise<string> {
    const r = await this.client.request<{ url: string }>('POST', `/api/v1/sessions/${this.id}/live`);
    return r.url;
  }
}

export class Baton {
  private readonly apiKey: string;
  private readonly baseUrl: string;

  constructor(opts: BatonOptions = {}) {
    // Read the environment where there is one (Node), without depending on Node's types.
    const env: Record<string, string | undefined> = (globalThis as { process?: { env: Record<string, string | undefined> } }).process?.env ?? {};
    this.apiKey = opts.apiKey ?? env.BATON_API_KEY ?? '';
    this.baseUrl = (opts.baseUrl ?? env.BATON_BASE_URL ?? 'https://batonvm.vercel.app').replace(/\/$/, '');
    if (!this.apiKey) {
      throw new BatonError('No API key: pass { apiKey } or set BATON_API_KEY (create one at https://batonvm.vercel.app/dashboard)', 401);
    }
  }

  readonly sessions = {
    /** Start a browser, optionally from a saved profile (logged in, on the pages it was saved on). */
    create: async (opts: { profile?: string } = {}): Promise<Session> =>
      new Session(this, await this.request<SessionData>('POST', '/api/v1/sessions', opts.profile ? { profile: opts.profile } : undefined)),
    list: async (): Promise<Session[]> =>
      (await this.request<SessionData[]>('GET', '/api/v1/sessions')).map((s) => new Session(this, s)),
    get: async (id: string): Promise<Session> => new Session(this, await this.request<SessionData>('GET', `/api/v1/sessions/${id}`)),
    delete: async (id: string): Promise<void> => {
      await this.request('DELETE', `/api/v1/sessions/${id}`);
    },
    migrate: (id: string): Promise<Migration> => this.request<Migration>('POST', `/api/v1/sessions/${id}/migrate`),
    pause: (id: string): Promise<Pause> => this.request<Pause>('POST', `/api/v1/sessions/${id}/pause`),
    resume: async (id: string): Promise<Session> =>
      new Session(this, await this.request<SessionData & { resumeMs?: number }>('POST', `/api/v1/sessions/${id}/resume`)),
    fork: async (id: string, count = 1): Promise<Fork> => {
      const f = await this.request<Omit<Fork, 'sessions'> & { sessions: SessionData[] }>('POST', `/api/v1/sessions/${id}/fork`, { count });
      return { ...f, sessions: f.sessions.map((s) => new Session(this, s)) };
    },
  };

  readonly profiles = {
    list: (): Promise<ProfileInfo[]> => this.request<ProfileInfo[]>('GET', '/api/v1/profiles'),
    save: (sessionId: string, name: string) =>
      this.request<{ name: string; pausedMs: number; uploadMs: number; storedMiB: number }>('POST', '/api/v1/profiles', { session: sessionId, name }),
    delete: async (name: string): Promise<void> => {
      await this.request('DELETE', `/api/v1/profiles/${encodeURIComponent(name)}`);
    },
  };

  usage(): Promise<Usage> {
    return this.request<Usage>('GET', '/api/v1/usage');
  }

  /** @internal */
  async request<T = unknown>(method: string, path: string, body?: unknown): Promise<T> {
    const headers: Record<string, string> = { Authorization: `Bearer ${this.apiKey}` };
    if (body !== undefined) headers['content-type'] = 'application/json';
    const res = await fetch(this.baseUrl + path, { method, headers, body: body === undefined ? undefined : JSON.stringify(body) });
    if (res.status === 204) return undefined as T;
    const data = await res.json().catch(() => ({}));
    if (!res.ok) throw new BatonError((data as { error?: string }).error ?? res.statusText, res.status);
    return data as T;
  }
}

export default Baton;
