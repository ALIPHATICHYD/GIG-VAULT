package gigs

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/aliphatichyd/gig-vault/backend/internal/auth"
	"github.com/aliphatichyd/gig-vault/backend/internal/store"
)

type User struct {
	ID       string  `json:"id"`
	Email    string  `json:"email"`
	Name     string  `json:"name"`
	Phone    *string `json:"phone"`
	Role     string  `json:"role"`
	WorkerID *string `json:"worker_id"`
}

type SignupInput struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	Name     string `json:"name"`
	Phone    string `json:"phone"`
	Role     string `json:"role"` // creator | client
}

func forbidden(msg string) error {
	return &AppError{HTTP: http.StatusForbidden, Code: "forbidden", Msg: msg}
}

func (s *Service) Signup(ctx context.Context, in SignupInput) (*User, error) {
	in.Email = strings.ToLower(strings.TrimSpace(in.Email))
	switch {
	case !strings.Contains(in.Email, "@"):
		return nil, badRequest("validation_error", "a valid email is required")
	case len(in.Password) < 6:
		return nil, badRequest("validation_error", "password must be at least 6 characters")
	case strings.TrimSpace(in.Name) == "":
		return nil, badRequest("validation_error", "name is required")
	case in.Role != string(auth.Creator) && in.Role != string(auth.Client):
		return nil, badRequest("validation_error", "role must be creator or client")
	}
	hash, err := auth.HashPassword(in.Password)
	if err != nil {
		return nil, err
	}
	id := store.NewID("USR")
	_, err = s.DB.Exec(ctx, `INSERT INTO users (id, email, name, phone, role, password_hash) VALUES ($1,$2,$3,NULLIF($4,''),$5,$6)`,
		id, in.Email, in.Name, in.Phone, in.Role, hash)
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return nil, conflict("email_taken", "an account with this email already exists, log in instead")
	}
	if err != nil {
		return nil, err
	}
	return s.GetUser(ctx, id)
}

func (s *Service) Login(ctx context.Context, email, password string) (*User, error) {
	var id, hash string
	err := s.DB.QueryRow(ctx, `SELECT id, password_hash FROM users WHERE email = $1`, strings.ToLower(strings.TrimSpace(email))).Scan(&id, &hash)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && !auth.CheckPassword(hash, password)) {
		return nil, &AppError{HTTP: http.StatusUnauthorized, Code: "invalid_credentials", Msg: "wrong email or password"}
	}
	if err != nil {
		return nil, err
	}
	return s.GetUser(ctx, id)
}

func (s *Service) GetUser(ctx context.Context, id string) (*User, error) {
	var u User
	err := s.DB.QueryRow(ctx, `SELECT id, email, name, phone, role, worker_id FROM users WHERE id = $1`, id).
		Scan(&u.ID, &u.Email, &u.Name, &u.Phone, &u.Role, &u.WorkerID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, notFound("user")
	}
	return &u, err
}

// WorkerFor returns the creator's worker id, or an error telling them to finish onboarding.
func (s *Service) WorkerFor(ctx context.Context, userID string) (string, error) {
	u, err := s.GetUser(ctx, userID)
	if err != nil {
		return "", err
	}
	if u.WorkerID == nil {
		return "", conflict("onboarding_required", "finish setting up your creator profile first")
	}
	return *u.WorkerID, nil
}

// OnboardCreator runs onboarding for a logged-in creator and links the worker to the account.
func (s *Service) OnboardCreator(ctx context.Context, userID string, in OnboardInput) (*OnboardResult, error) {
	u, err := s.GetUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	if u.WorkerID != nil {
		return nil, conflict("already_onboarded", "your creator profile is already set up")
	}
	in.Name, in.Email = def(in.Name, u.Name), def(in.Email, u.Email)
	if in.Phone == "" && u.Phone != nil {
		in.Phone = *u.Phone
	}
	res, err := s.Onboard(ctx, in)
	if err != nil {
		return nil, err
	}
	if _, err := s.DB.Exec(ctx, `UPDATE users SET worker_id = $2 WHERE id = $1`, userID, res.Worker.ID); err != nil {
		return nil, err
	}
	return res, nil
}

// ---------- ownership checks: each side only sees its own gigs ----------

func (s *Service) CreatorGig(ctx context.Context, userID, gigID string) (*GigDetail, error) {
	wid, err := s.WorkerFor(ctx, userID)
	if err != nil {
		return nil, err
	}
	d, err := s.GetGig(ctx, gigID)
	if err != nil {
		return nil, err
	}
	if d.Gig.WorkerID != wid {
		return nil, notFound("gig")
	}
	return d, nil
}

// ClaimGig attaches a gig link to the logged-in client. The link token proves
// they were sent it; the first client to claim it owns the booking.
func (s *Service) ClaimGig(ctx context.Context, userID, gigID, token string) (*PublicGig, error) {
	if err := s.CheckToken(ctx, gigID, token); err != nil {
		return nil, err
	}
	tag, err := s.DB.Exec(ctx, `UPDATE gigs SET client_user_id = $2, updated_at = now()
		WHERE id = $1 AND (client_user_id IS NULL OR client_user_id = $2)`, gigID, userID)
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() == 0 {
		return nil, conflict("already_claimed", "this gig was already booked by another client")
	}
	_ = store.Audit(ctx, s.DB, gigID, "", "client", "gig.claimed_by_client", "", "", map[string]string{"user_id": userID})
	return s.ClientGig(ctx, userID, gigID)
}

func (s *Service) ownClientGig(ctx context.Context, userID, gigID string) (*Gig, error) {
	g, err := getGig(ctx, s.DB, gigID, false)
	if err != nil {
		return nil, err
	}
	var owner *string
	if err := s.DB.QueryRow(ctx, `SELECT client_user_id FROM gigs WHERE id = $1`, gigID).Scan(&owner); err != nil {
		return nil, err
	}
	if owner == nil || *owner != userID {
		return nil, notFound("gig")
	}
	return g, nil
}

func (s *Service) ClientGig(ctx context.Context, userID, gigID string) (*PublicGig, error) {
	g, err := s.ownClientGig(ctx, userID, gigID)
	if err != nil {
		return nil, err
	}
	return s.PublicGig(ctx, gigID, g.ClientToken)
}

func (s *Service) ClientCheckout(ctx context.Context, userID, gigID, currency string) (*PublicGig, error) {
	g, err := s.ownClientGig(ctx, userID, gigID)
	if err != nil {
		return nil, err
	}
	u, err := s.GetUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	return s.Checkout(ctx, gigID, g.ClientToken, CheckoutInput{ClientName: u.Name, ClientEmail: u.Email, Currency: currency})
}

// ClientAction runs complete / complain / cancel after checking ownership.
func (s *Service) ClientAction(ctx context.Context, userID, gigID, action, reason string) (*PublicGig, error) {
	g, err := s.ownClientGig(ctx, userID, gigID)
	if err != nil {
		return nil, err
	}
	switch action {
	case "complete":
		_, err = s.Complete(ctx, gigID)
	case "complain":
		_, err = s.Complain(ctx, gigID, reason)
	case "cancel":
		_, err = s.Cancel(ctx, gigID)
	case "accept_split":
		err = s.AcceptSplit(ctx, gigID, "client")
	default:
		return nil, badRequest("validation_error", "unknown action")
	}
	if err != nil {
		return nil, err
	}
	return s.PublicGig(ctx, gigID, g.ClientToken)
}

func (s *Service) ClientBookings(ctx context.Context, userID string) ([]*PublicGig, error) {
	// Invoices addressed to this client's email show up without needing the link.
	if _, err := s.DB.Exec(ctx, `UPDATE gigs g SET client_user_id = u.id FROM users u
		WHERE u.id = $1 AND g.client_user_id IS NULL AND g.client_email = lower(u.email)`, userID); err != nil {
		return nil, err
	}
	rows, err := s.DB.Query(ctx, `SELECT id, client_token FROM gigs WHERE client_user_id = $1 ORDER BY created_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	type pair struct{ id, tok string }
	var ps []pair
	for rows.Next() {
		var p pair
		if err := rows.Scan(&p.id, &p.tok); err != nil {
			rows.Close()
			return nil, err
		}
		ps = append(ps, p)
	}
	rows.Close()
	out := []*PublicGig{}
	for _, p := range ps {
		g, err := s.PublicGig(ctx, p.id, p.tok)
		if err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, nil
}

// ClientProposeSplit lets the client propose a split of the frozen money.
func (s *Service) ClientProposeSplit(ctx context.Context, userID, gigID string, pct int) (*PublicGig, error) {
	g, err := s.ownClientGig(ctx, userID, gigID)
	if err != nil {
		return nil, err
	}
	if err := s.ProposeSplit(ctx, gigID, "client", pct); err != nil {
		return nil, err
	}
	return s.PublicGig(ctx, gigID, g.ClientToken)
}

// CreatorDispute lets the creator propose or accept a split on her own gig.
func (s *Service) CreatorDispute(ctx context.Context, userID, gigID, action string, pct int) (*GigDetail, error) {
	if _, err := s.CreatorGig(ctx, userID, gigID); err != nil {
		return nil, err
	}
	var err error
	if action == "propose" {
		err = s.ProposeSplit(ctx, gigID, "creator", pct)
	} else {
		err = s.AcceptSplit(ctx, gigID, "creator")
	}
	if err != nil {
		return nil, err
	}
	return s.GetGig(ctx, gigID)
}

var _ = forbidden
