package api

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"tf-automation/backend/internal/auth"
	"tf-automation/backend/internal/data"
)

// userLookupAdapter implements auth.UserLookup (WS-F's small decoupling
// interface) backed by WS-A's real users.GetByUsername-equivalent,
// data.Store.GetUserByUsername. This is the "small adapter" WS-G is
// responsible for per the task brief -- WS-F deliberately doesn't depend on
// WS-A's concrete package so it can be built/tested in isolation.
type userLookupAdapter struct {
	store *data.Store
}

func (a *userLookupAdapter) GetByUsername(ctx context.Context, username string) (id uuid.UUID, passwordHash string, isAdmin bool, roles []string, err error) {
	u, err := a.store.GetUserByUsername(ctx, username)
	if err != nil {
		if errors.Is(err, data.ErrNotFound) {
			return uuid.Nil, "", false, nil, auth.ErrUserNotFound
		}
		return uuid.Nil, "", false, nil, err
	}
	roles, err = a.store.GetUserRoles(ctx, u.ID)
	if err != nil {
		return uuid.Nil, "", false, nil, err
	}
	return u.ID, u.PasswordHash, u.IsAdmin, roles, nil
}
