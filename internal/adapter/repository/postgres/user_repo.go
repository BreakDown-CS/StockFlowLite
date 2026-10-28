package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/BreakDown-CS/StockFlowLite/internal/domain"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// constraintUsernameUnique is the index name created by 000001_create_users.up.sql.
const constraintUsernameUnique = "users_username_key"

// UserRepository implements domain.UserRepository using pgxpool.
type UserRepository struct {
	pool *pgxpool.Pool
}

// NewUserRepository constructs a UserRepository.
func NewUserRepository(pool *pgxpool.Pool) *UserRepository {
	return &UserRepository{pool: pool}
}

const userColumns = `id, username, password_hash, role, is_active, last_login_at, password_changed_at, created_at, updated_at`

func scanUser(row rowScanner) (*domain.User, error) {
	var user domain.User
	var role string

	err := row.Scan(
		&user.ID,
		&user.Username,
		&user.PasswordHash,
		&role,
		&user.IsActive,
		&user.LastLoginAt,
		&user.PasswordChangedAt,
		&user.CreatedAt,
		&user.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}

	user.Role = domain.UserRole(role)
	return &user, nil
}

// Create inserts a new account. Duplicate usernames map to ErrDuplicateUsername.
func (r *UserRepository) Create(ctx context.Context, u *domain.User) error {
	const query = `
		INSERT INTO users (id, username, password_hash, role, is_active, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7);
	`

	_, err := r.pool.Exec(ctx, query, u.ID, u.Username, u.PasswordHash, string(u.Role), u.IsActive, u.CreatedAt, u.UpdatedAt)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgErrUniqueViolation && pgErr.ConstraintName == constraintUsernameUnique {
			return domain.ErrDuplicateUsername
		}
		return fmt.Errorf("failed to insert user: %w", err)
	}

	return nil
}

// GetByID loads a single account.
func (r *UserRepository) GetByID(ctx context.Context, id uuid.UUID) (*domain.User, error) {
	query := fmt.Sprintf("SELECT %s FROM users WHERE id = $1;", userColumns)

	user, err := scanUser(r.pool.QueryRow(ctx, query, id))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrUserNotFound
		}
		return nil, fmt.Errorf("failed to query user by id: %w", err)
	}
	return user, nil
}

// GetByUsername loads a single account by its unique username (case-insensitive).
func (r *UserRepository) GetByUsername(ctx context.Context, username string) (*domain.User, error) {
	query := fmt.Sprintf("SELECT %s FROM users WHERE lower(username) = lower($1);", userColumns)

	user, err := scanUser(r.pool.QueryRow(ctx, query, strings.TrimSpace(username)))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrUserNotFound
		}
		return nil, fmt.Errorf("failed to query user by username: %w", err)
	}
	return user, nil
}

// List returns a filtered, paginated page of accounts plus the total count.
func (r *UserRepository) List(ctx context.Context, filter domain.ListUserFilter) ([]domain.User, int64, error) {
	limit := filter.Limit
	if limit <= 0 {
		limit = 20
	} else if limit > 100 {
		limit = 100
	}
	offset := filter.Offset
	if offset < 0 {
		offset = 0
	}

	where := []string{"1=1"}
	args := make([]any, 0, 4)
	argIdx := 1

	if search := strings.TrimSpace(filter.Search); search != "" {
		where = append(where, fmt.Sprintf("username ILIKE $%d", argIdx))
		args = append(args, "%"+search+"%")
		argIdx++
	}
	if filter.Role != nil {
		where = append(where, fmt.Sprintf("role = $%d", argIdx))
		args = append(args, string(*filter.Role))
		argIdx++
	}
	if filter.IsActive != nil {
		where = append(where, fmt.Sprintf("is_active = $%d", argIdx))
		args = append(args, *filter.IsActive)
		argIdx++
	}

	whereClause := strings.Join(where, " AND ")

	var total int64
	countQuery := fmt.Sprintf("SELECT COUNT(*) FROM users WHERE %s", whereClause)
	if err := r.pool.QueryRow(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("failed to count users: %w", err)
	}

	dataQuery := fmt.Sprintf(`
		SELECT %s FROM users
		WHERE %s
		ORDER BY username ASC
		LIMIT $%d OFFSET $%d;
	`, userColumns, whereClause, argIdx, argIdx+1)

	rows, err := r.pool.Query(ctx, dataQuery, append(args, limit, offset)...)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to query users: %w", err)
	}
	defer rows.Close()

	users := make([]domain.User, 0, limit)
	for rows.Next() {
		user, err := scanUser(rows)
		if err != nil {
			return nil, 0, fmt.Errorf("failed to scan user row: %w", err)
		}
		users = append(users, *user)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("error during users iteration: %w", err)
	}

	return users, total, nil
}

// UpdateRole changes the role of an account.
func (r *UserRepository) UpdateRole(ctx context.Context, id uuid.UUID, role domain.UserRole) error {
	const query = `UPDATE users SET role = $2, updated_at = now() WHERE id = $1;`

	tag, err := r.pool.Exec(ctx, query, id, string(role))
	if err != nil {
		return fmt.Errorf("failed to update user role: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrUserNotFound
	}
	return nil
}

// SetActive enables or disables an account.
func (r *UserRepository) SetActive(ctx context.Context, id uuid.UUID, isActive bool) error {
	const query = `UPDATE users SET is_active = $2, updated_at = now() WHERE id = $1;`

	tag, err := r.pool.Exec(ctx, query, id, isActive)
	if err != nil {
		return fmt.Errorf("failed to update user status: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrUserNotFound
	}
	return nil
}

// UpdatePasswordHash stores a new password hash and marks when it changed.
func (r *UserRepository) UpdatePasswordHash(ctx context.Context, id uuid.UUID, passwordHash string, changedAt time.Time) error {
	const query = `
		UPDATE users
		SET password_hash = $2, password_changed_at = $3, updated_at = $3
		WHERE id = $1;
	`

	tag, err := r.pool.Exec(ctx, query, id, passwordHash, changedAt)
	if err != nil {
		return fmt.Errorf("failed to update password hash: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrUserNotFound
	}
	return nil
}

// TouchLastLogin records a successful authentication (best-effort, never fails the login).
func (r *UserRepository) TouchLastLogin(ctx context.Context, id uuid.UUID, at time.Time) error {
	const query = `UPDATE users SET last_login_at = $2 WHERE id = $1;`

	if _, err := r.pool.Exec(ctx, query, id, at); err != nil {
		return fmt.Errorf("failed to record last login: %w", err)
	}
	return nil
}

// CountActiveAdmins reports how many enabled administrators remain (lockout guard).
func (r *UserRepository) CountActiveAdmins(ctx context.Context) (int, error) {
	const query = `SELECT COUNT(*) FROM users WHERE role = 'ADMIN' AND is_active = true;`

	var count int
	if err := r.pool.QueryRow(ctx, query).Scan(&count); err != nil {
		return 0, fmt.Errorf("failed to count active administrators: %w", err)
	}
	return count, nil
}
