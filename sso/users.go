package sso

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
)

// User is one end-user within the calling product's scope.
type User struct {
	CentralID     string `json:"centralId"`
	Email         string `json:"email"`
	EmailVerified bool   `json:"emailVerified"`
	FirstName     string `json:"firstName,omitempty"`
	LastName      string `json:"lastName,omitempty"`
	Status        string `json:"status"` // active | suspended | locked
	OnboardedAt   string `json:"onboardedAt,omitempty"`
	OnboardedBy   string `json:"onboardedBy,omitempty"`
}

// CreateUserRequest asks for a new end-user identity + invitation.
type CreateUserRequest struct {
	Email     string `json:"email"`
	FirstName string `json:"firstName,omitempty"`
	LastName  string `json:"lastName,omitempty"`
	// InviteMode: "email" (default) sends the invitation via the central
	// mailer; "returnCode" returns the verification code to the caller.
	InviteMode string `json:"inviteMode,omitempty"`
}

// CreateUserResponse reports the created identity.
type CreateUserResponse struct {
	CentralID        string `json:"centralId"`
	Created          bool   `json:"created"`
	Onboarded        bool   `json:"onboarded"`
	VerificationCode string `json:"verificationCode,omitempty"`
}

// CreateUser creates a central identity and sends the invitation. If the
// email already belongs to an identity it fails with a 409 *APIError — use
// its ExistingCentralID with Onboard instead.
func (c *Client) CreateUser(ctx context.Context, req *CreateUserRequest, opts ...CallOption) (*CreateUserResponse, error) {
	var out CreateUserResponse
	if err := c.do(ctx, http.MethodPost, "/users", nil, req, &out, true, opts...); err != nil {
		return nil, err
	}
	return &out, nil
}

// OnboardRequest targets an EXISTING identity by centralId or VERIFIED
// email (exactly one; unverified emails never match).
type OnboardRequest struct {
	CentralID string `json:"centralId,omitempty"`
	Email     string `json:"email,omitempty"`
}

// OnboardResponse confirms membership.
type OnboardResponse struct {
	CentralID string `json:"centralId"`
	Onboarded bool   `json:"onboarded"` // false: was already onboarded (no-op)
}

// Onboard marks an existing identity as a member of the calling product —
// the primary cross-product operation. Idempotent.
func (c *Client) Onboard(ctx context.Context, req *OnboardRequest, opts ...CallOption) (*OnboardResponse, error) {
	var out OnboardResponse
	if err := c.do(ctx, http.MethodPost, "/users/onboard", nil, req, &out, true, opts...); err != nil {
		return nil, err
	}
	return &out, nil
}

// UserList is one page of the product's onboarded users.
type UserList struct {
	Users      []User `json:"users"`
	NextCursor string `json:"nextCursor,omitempty"`
}

// ListUsersParams filters and paginates ListUsers. Zero values mean
// server defaults.
type ListUsersParams struct {
	Cursor      string
	Limit       int
	EmailPrefix string
}

// ListUsers pages through the product's onboarded users.
func (c *Client) ListUsers(ctx context.Context, params *ListUsersParams) (*UserList, error) {
	q := url.Values{}
	if params != nil {
		if params.Cursor != "" {
			q.Set("cursor", params.Cursor)
		}
		if params.Limit > 0 {
			q.Set("limit", strconv.Itoa(params.Limit))
		}
		if params.EmailPrefix != "" {
			q.Set("email", params.EmailPrefix)
		}
	}
	var out UserList
	if err := c.do(ctx, http.MethodGet, "/users", q, nil, &out, false); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetUser returns one user's detail within the product scope (404 outside it).
func (c *Client) GetUser(ctx context.Context, centralID string) (*User, error) {
	var out User
	if err := c.do(ctx, http.MethodGet, "/users/"+url.PathEscape(centralID), nil, nil, &out, false); err != nil {
		return nil, err
	}
	return &out, nil
}

// ChangeEmailRequest replaces the user's email (GLOBAL effect — the email
// is the person's cross-product identity key). The new address starts
// unverified.
type ChangeEmailRequest struct {
	Email string `json:"email"`
	// VerifyMode: "email" (default) sends the verification centrally;
	// "returnCode" returns the code to the caller.
	VerifyMode string `json:"verifyMode,omitempty"`
}

// ChangeEmailResponse confirms the change.
type ChangeEmailResponse struct {
	CentralID        string `json:"centralId"`
	Email            string `json:"email"`
	VerificationCode string `json:"verificationCode,omitempty"`
	GlobalEffect     bool   `json:"globalEffect"`
}

// ChangeEmail replaces the user's email address (GLOBAL effect). Product
// IdP records keying on the old address must be updated by their owners.
func (c *Client) ChangeEmail(ctx context.Context, centralID string, req *ChangeEmailRequest, opts ...CallOption) (*ChangeEmailResponse, error) {
	var out ChangeEmailResponse
	if err := c.do(ctx, http.MethodPost, "/users/"+url.PathEscape(centralID)+"/email", nil, req, &out, true, opts...); err != nil {
		return nil, err
	}
	return &out, nil
}

// ActionResult acknowledges a state-changing support operation.
type ActionResult struct {
	CentralID string `json:"centralId"`
	Action    string `json:"action"`
	// GlobalEffect: the action affected the user across ALL products, not
	// only the caller's (one person - one central identity).
	GlobalEffect bool `json:"globalEffect"`
}

// SendVerificationEmail resends the invitation / verification email.
// 409 when the email is already verified.
func (c *Client) SendVerificationEmail(ctx context.Context, centralID string) (*ActionResult, error) {
	return c.action(ctx, centralID, "/verification-email")
}

// ResetMFA removes the user's second factors (GLOBAL effect); the user
// re-enrolls at next login.
func (c *Client) ResetMFA(ctx context.Context, centralID string) (*ActionResult, error) {
	return c.action(ctx, centralID, "/mfa/reset")
}

// ResetPassword starts a central password reset (GLOBAL effect).
func (c *Client) ResetPassword(ctx context.Context, centralID string) (*ActionResult, error) {
	return c.action(ctx, centralID, "/password/reset")
}

// Lock deactivates the central identity: no new logins anywhere (GLOBAL
// effect). Product sessions already issued live until they expire.
func (c *Client) Lock(ctx context.Context, centralID string) (*ActionResult, error) {
	return c.action(ctx, centralID, "/lock")
}

// Unlock reactivates a locked identity (GLOBAL effect).
func (c *Client) Unlock(ctx context.Context, centralID string) (*ActionResult, error) {
	return c.action(ctx, centralID, "/unlock")
}

func (c *Client) action(ctx context.Context, centralID, suffix string) (*ActionResult, error) {
	var out ActionResult
	if err := c.do(ctx, http.MethodPost, "/users/"+url.PathEscape(centralID)+suffix, nil, nil, &out, true); err != nil {
		return nil, err
	}
	return &out, nil
}

// Session is an active central session of a user.
type Session struct {
	SessionID string `json:"sessionId"`
	CreatedAt string `json:"createdAt"`
	UserAgent string `json:"userAgent,omitempty"`
}

// SessionList lists a user's central sessions.
type SessionList struct {
	Sessions []Session `json:"sessions"`
}

// ListSessions lists the user's central sessions.
func (c *Client) ListSessions(ctx context.Context, centralID string) (*SessionList, error) {
	var out SessionList
	if err := c.do(ctx, http.MethodGet, "/users/"+url.PathEscape(centralID)+"/sessions", nil, nil, &out, false); err != nil {
		return nil, err
	}
	return &out, nil
}

// TerminateSession ends one central session.
func (c *Client) TerminateSession(ctx context.Context, centralID, sessionID string) error {
	return c.do(ctx, http.MethodDelete, "/users/"+url.PathEscape(centralID)+"/sessions/"+url.PathEscape(sessionID), nil, nil, nil, true)
}

// TerminateAllSessions ends every central session of the user.
func (c *Client) TerminateAllSessions(ctx context.Context, centralID string) error {
	return c.do(ctx, http.MethodDelete, "/users/"+url.PathEscape(centralID)+"/sessions", nil, nil, nil, true)
}

// AuditEvent is one entry of a user's audit timeline within the product scope.
type AuditEvent struct {
	Timestamp    string `json:"timestamp"`
	RequestID    string `json:"requestId"`
	Action       string `json:"action"`
	Outcome      string `json:"outcome"`
	Actor        string `json:"actor"`
	GlobalEffect bool   `json:"globalEffect"`
}

// AuditList is one page of audit events.
type AuditList struct {
	Events     []AuditEvent `json:"events"`
	NextCursor string       `json:"nextCursor,omitempty"`
}

// GetAudit pages through the user's audit timeline within the product scope.
func (c *Client) GetAudit(ctx context.Context, centralID string, params *ListUsersParams) (*AuditList, error) {
	q := url.Values{}
	if params != nil {
		if params.Cursor != "" {
			q.Set("cursor", params.Cursor)
		}
		if params.Limit > 0 {
			q.Set("limit", strconv.Itoa(params.Limit))
		}
	}
	var out AuditList
	if err := c.do(ctx, http.MethodGet, "/users/"+url.PathEscape(centralID)+"/audit", q, nil, &out, false); err != nil {
		return nil, err
	}
	return &out, nil
}
