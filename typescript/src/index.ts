/**
 * TypeScript (Node.js) client for the SSO administration API: every operation
 * is scoped to one product, authenticated with the product's machine key.
 * Tokens are minted (JWT profile) and refreshed automatically.
 */
import { createPrivateKey } from "node:crypto";
import { randomUUID } from "node:crypto";
import { SignJWT } from "jose";

/** The machine key JSON issued to your product (keep it in a secret store). */
export interface MachineKey {
  keyId: string;
  key: string; // PEM
  userId: string;
}

/** Per-product connection settings, issued during product onboarding. */
export interface Config {
  /** Administration API base URL. */
  endpoint: string;
  /** Registered product identifier; must match the credentials. */
  product: string;
  /** Central identity issuer (https://<domain>). */
  issuer: string;
  /** Central API project id — tokens carry its audience. */
  projectId: string;
  /** Machine key JSON (object or raw string). */
  key: MachineKey | string;
  /** Optional fetch override (instrumentation, testing). */
  fetch?: typeof fetch;
}

export interface User {
  centralId: string;
  email: string;
  emailVerified: boolean;
  firstName?: string;
  lastName?: string;
  status: string;
  onboardedAt?: string;
  onboardedBy?: string;
}

export interface CreateUserRequest {
  email: string;
  firstName?: string;
  lastName?: string;
  /** "email" (default) mails the invitation; "returnCode" returns the code. */
  inviteMode?: "email" | "returnCode";
}

export interface CreateUserResponse {
  centralId: string;
  created: boolean;
  onboarded: boolean;
  verificationCode?: string;
}

/** Exactly one of centralId / email; unverified emails never match. */
export interface OnboardRequest {
  centralId?: string;
  email?: string;
}

export interface OnboardResponse {
  centralId: string;
  onboarded: boolean;
}

export interface UserList {
  users: User[];
  nextCursor?: string;
}

export interface ListUsersParams {
  cursor?: string;
  limit?: number;
  emailPrefix?: string;
}

export interface ActionResult {
  centralId: string;
  action: string;
  /** True: affected the user across ALL products, not only the caller's. */
  globalEffect: boolean;
}

export interface Session {
  sessionId: string;
  createdAt: string;
  userAgent?: string;
}

export interface SessionList {
  sessions: Session[];
}

export interface AuditEvent {
  timestamp: string;
  requestId?: string;
  action: string;
  outcome: string;
  actor: string;
  globalEffect: boolean;
}

export interface AuditList {
  events: AuditEvent[];
  nextCursor?: string;
}

interface ErrorDetail {
  message?: string;
  location?: string;
  value?: unknown;
}

/** A non-2xx response (RFC 9457 problem shape). */
export class ApiError extends Error {
  constructor(
    public readonly status: number,
    public readonly title: string,
    public readonly detail: string,
    public readonly errors: ErrorDetail[] = [],
  ) {
    super(`sso: ${status} ${title}: ${detail}`);
    this.name = "ApiError";
  }

  /**
   * The already-existing identity id from a create-user conflict —
   * use it with onboard() instead. Empty when absent.
   */
  existingCentralId(): string {
    if (this.status !== 409) return "";
    for (const d of this.errors) {
      if (typeof d.value === "string" && d.value !== "") return d.value;
    }
    return "";
  }
}

/** Options for one mutating call. */
export interface CallOptions {
  /** Pin the Idempotency-Key to make your own retries exact replays. */
  idempotencyKey?: string;
}

/** Product-scoped handle on the administration API. Safe for concurrent use. */
export class Client {
  private readonly endpoint: string;
  private readonly product: string;
  private readonly issuer: string;
  private readonly scope: string;
  private readonly key: MachineKey;
  private readonly fetch: typeof fetch;
  private token?: { value: string; expiresAt: number };

  constructor(cfg: Config) {
    for (const f of ["endpoint", "product", "issuer", "projectId", "key"] as const) {
      if (!cfg[f]) throw new Error(`sso: ${f} is required`);
    }
    this.endpoint = cfg.endpoint.replace(/\/+$/, "");
    this.product = cfg.product;
    this.issuer = cfg.issuer.replace(/\/+$/, "");
    this.scope = `openid urn:zitadel:iam:org:project:id:${cfg.projectId}:aud`;
    this.key = typeof cfg.key === "string" ? (JSON.parse(cfg.key) as MachineKey) : cfg.key;
    this.fetch = cfg.fetch ?? fetch;
  }

  /** Creates the central identity and sends the invitation (409 → onboard). */
  createUser(req: CreateUserRequest, opts?: CallOptions): Promise<CreateUserResponse> {
    return this.do("POST", "/users", { body: req, mutation: true, opts });
  }

  /** Marks an EXISTING identity as a member of the calling product. */
  onboard(req: OnboardRequest, opts?: CallOptions): Promise<OnboardResponse> {
    return this.do("POST", "/users/onboard", { body: req, mutation: true, opts });
  }

  /** Pages through the product's onboarded users. */
  listUsers(params?: ListUsersParams): Promise<UserList> {
    const q = new URLSearchParams();
    if (params?.cursor) q.set("cursor", params.cursor);
    if (params?.limit) q.set("limit", String(params.limit));
    if (params?.emailPrefix) q.set("email", params.emailPrefix);
    return this.do("GET", "/users", { query: q });
  }

  /** One user's detail within the product scope (404 outside it). */
  getUser(centralId: string): Promise<User> {
    return this.do("GET", `/users/${encodeURIComponent(centralId)}`, {});
  }

  /** Resend the invitation / verification email (409 if already verified). */
  sendVerificationEmail(centralId: string): Promise<ActionResult> {
    return this.action(centralId, "/verification-email");
  }

  /** Remove the user's second factors (GLOBAL effect). */
  resetMfa(centralId: string): Promise<ActionResult> {
    return this.action(centralId, "/mfa/reset");
  }

  /** Start a central password reset (GLOBAL effect). */
  resetPassword(centralId: string): Promise<ActionResult> {
    return this.action(centralId, "/password/reset");
  }

  /** Deactivate the central identity (GLOBAL effect). */
  lock(centralId: string): Promise<ActionResult> {
    return this.action(centralId, "/lock");
  }

  /** Reactivate a locked identity (GLOBAL effect). */
  unlock(centralId: string): Promise<ActionResult> {
    return this.action(centralId, "/unlock");
  }

  /** List the user's central sessions. */
  listSessions(centralId: string): Promise<SessionList> {
    return this.do("GET", `/users/${encodeURIComponent(centralId)}/sessions`, {});
  }

  /** Terminate one central session. */
  async terminateSession(centralId: string, sessionId: string): Promise<void> {
    await this.do(
      "DELETE",
      `/users/${encodeURIComponent(centralId)}/sessions/${encodeURIComponent(sessionId)}`,
      { mutation: true },
    );
  }

  /** Terminate every central session of the user. */
  async terminateAllSessions(centralId: string): Promise<void> {
    await this.do("DELETE", `/users/${encodeURIComponent(centralId)}/sessions`, { mutation: true });
  }

  /** The user's audit timeline within the product scope. */
  getAudit(centralId: string, params?: ListUsersParams): Promise<AuditList> {
    const q = new URLSearchParams();
    if (params?.cursor) q.set("cursor", params.cursor);
    if (params?.limit) q.set("limit", String(params.limit));
    return this.do("GET", `/users/${encodeURIComponent(centralId)}/audit`, { query: q });
  }

  private action(centralId: string, suffix: string): Promise<ActionResult> {
    return this.do("POST", `/users/${encodeURIComponent(centralId)}${suffix}`, { mutation: true });
  }

  private async accessToken(): Promise<string> {
    if (this.token && Date.now() < this.token.expiresAt - 120_000) {
      return this.token.value;
    }
    const pk = createPrivateKey(this.key.key); // handles PKCS#1 and PKCS#8 PEM
    const assertion = await new SignJWT({})
      .setProtectedHeader({ alg: "RS256", kid: this.key.keyId })
      .setIssuer(this.key.userId)
      .setSubject(this.key.userId)
      .setAudience(this.issuer)
      .setIssuedAt()
      .setExpirationTime("10m")
      .sign(pk);
    const resp = await this.fetch(`${this.issuer}/oauth/v2/token`, {
      method: "POST",
      headers: { "Content-Type": "application/x-www-form-urlencoded" },
      body: new URLSearchParams({
        grant_type: "urn:ietf:params:oauth:grant-type:jwt-bearer",
        scope: this.scope,
        assertion,
      }),
    });
    if (!resp.ok) {
      throw new ApiError(resp.status, "TokenError", await resp.text());
    }
    const data = (await resp.json()) as { access_token: string; expires_in: number };
    this.token = { value: data.access_token, expiresAt: Date.now() + data.expires_in * 1000 };
    return this.token.value;
  }

  private async do<T>(
    method: string,
    path: string,
    args: { body?: unknown; query?: URLSearchParams; mutation?: boolean; opts?: CallOptions },
  ): Promise<T> {
    const token = await this.accessToken();
    let url = `${this.endpoint}/v1/products/${encodeURIComponent(this.product)}${path}`;
    if (args.query && [...args.query].length > 0) url += `?${args.query}`;
    const headers: Record<string, string> = { Authorization: `Bearer ${token}` };
    if (args.body !== undefined) headers["Content-Type"] = "application/json";
    if (args.mutation) headers["Idempotency-Key"] = args.opts?.idempotencyKey ?? randomUUID();
    const resp = await this.fetch(url, {
      method,
      headers,
      body: args.body !== undefined ? JSON.stringify(args.body) : undefined,
    });
    const text = await resp.text();
    if (!resp.ok) {
      try {
        const p = JSON.parse(text) as {
          status?: number;
          title?: string;
          detail?: string;
          errors?: ErrorDetail[];
        };
        throw new ApiError(resp.status, p.title ?? resp.statusText, p.detail ?? "", p.errors ?? []);
      } catch (e) {
        if (e instanceof ApiError) throw e;
        throw new ApiError(resp.status, resp.statusText, text);
      }
    }
    return (text ? JSON.parse(text) : undefined) as T;
  }
}
